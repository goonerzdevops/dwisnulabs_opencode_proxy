package proxy

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tls_client "github.com/bogdanfinn/tls-client"
	xproxy "golang.org/x/net/proxy"

	"opencode-proxy/internal/config"
)

// WarpTraceInfo holds the verified egress identity of a live wireproxy tunnel.
type WarpTraceInfo struct {
	IP          string `json:"ip"`
	Prefix64    string `json:"prefix_64"`
	Colo        string `json:"colo"`
	Loc         string `json:"loc"`
	WarpStatus  string `json:"warp_status"`
	Endpoint    string `json:"endpoint"`
	LocalPort   int    `json:"local_port"`
	ProxyURL    string `json:"proxy_url"`
	HandshakeMs int64  `json:"handshake_ms"`
}

// WarpTunnelInstance encapsulates one running wireproxy child process and its HTTP/TLS clients.
// It tracks active in-flight streams so retiring a tunnel waits for live responses to finish
// cleanly instead of cutting SSE streams mid-flight.
type WarpTunnelInstance struct {
	Generation  uint64
	Trace       WarpTraceInfo
	ConnectedAt time.Time
	tlsClient   tls_client.HttpClient
	stdClient   *http.Client
	inFlight    atomic.Int32
	retiring    atomic.Bool
	stopOnce    sync.Once
	stopFn      func()
}

// Acquire claims an in-flight slot on this tunnel. Returns false if the tunnel is retiring.
func (t *WarpTunnelInstance) Acquire() bool {
	if t == nil || t.retiring.Load() {
		return false
	}
	t.inFlight.Add(1)
	if t.retiring.Load() {
		t.Release()
		return false
	}
	return true
}

// Release frees an in-flight slot and stops the tunnel immediately if it is retiring and drained.
func (t *WarpTunnelInstance) Release() {
	if t == nil {
		return
	}
	rem := t.inFlight.Add(-1)
	if rem <= 0 && t.retiring.Load() {
		t.Stop()
	}
}

// InFlight returns the number of active requests currently streaming through this tunnel.
func (t *WarpTunnelInstance) InFlight() int32 {
	if t == nil {
		return 0
	}
	return t.inFlight.Load()
}

// Retire marks this tunnel as replaced so new requests will not enter it, and stops the
// underlying wireproxy process as soon as in-flight streams drain (with a 45s hard cap).
func (t *WarpTunnelInstance) Retire() {
	if t == nil {
		return
	}
	t.retiring.Store(true)
	if t.inFlight.Load() <= 0 {
		t.Stop()
		return
	}
	go func() {
		timer := time.NewTimer(45 * time.Second)
		defer timer.Stop()
		<-timer.C
		t.Stop()
	}()
}

// Stop immediately terminates the underlying wireproxy child process and removes its temporary config file.
func (t *WarpTunnelInstance) Stop() {
	if t == nil {
		return
	}
	t.retiring.Store(true)
	t.stopOnce.Do(func() {
		if t.stopFn != nil {
			t.stopFn()
		}
	})
}

// ResolveWireproxyBinary finds an existing wireproxy binary or auto-downloads it if enabled.
func ResolveWireproxyBinary(ctx context.Context, cfg config.Config, dataDir string) (string, error) {
	if custom := strings.TrimSpace(cfg.WarpWireproxyPath); custom != "" {
		if info, err := os.Stat(custom); err == nil && !info.IsDir() {
			return custom, nil
		}
		return "", fmt.Errorf("configured warp_wireproxy_path not found: %s", custom)
	}

	binName := "wireproxy"
	if runtime.GOOS == "windows" {
		binName = "wireproxy.exe"
	}
	managedPath := filepath.Join(dataDir, binName)
	if info, err := os.Stat(managedPath); err == nil && !info.IsDir() && info.Size() > 0 {
		return managedPath, nil
	}

	if path, err := exec.LookPath(binName); err == nil && path != "" {
		return path, nil
	}

	if !cfg.WarpAutoDownload {
		return "", fmt.Errorf("%s not found in %s and warp_auto_download is disabled", binName, dataDir)
	}

	if err := downloadWireproxyRelease(ctx, managedPath); err != nil {
		return "", fmt.Errorf("auto-download wireproxy: %w", err)
	}
	return managedPath, nil
}

func startWireproxyTunnel(ctx context.Context, cfg config.Config, store *WarpStore, dev *WarpDevice, endpoint string) (*WarpTunnelInstance, error) {
	binPath, err := ResolveWireproxyBinary(ctx, cfg, store.DataDir())
	if err != nil {
		return nil, err
	}

	port, err := allocateFreeLocalPort()
	if err != nil {
		return nil, fmt.Errorf("allocate local socks port: %w", err)
	}

	if len(dev.Reg.Config.Peers) == 0 {
		return nil, fmt.Errorf("warp device has no peers")
	}
	peerPub := dev.Reg.Config.Peers[0].PublicKey
	v4 := strings.TrimSpace(dev.Reg.Config.Interface.Addresses.V4)
	v6 := strings.TrimSpace(dev.Reg.Config.Interface.Addresses.V6)
	if v6 == "" {
		return nil, fmt.Errorf("warp device missing IPv6 interface address")
	}
	addrLine := fmt.Sprintf("%s/128", v6)
	if v4 != "" {
		addrLine = fmt.Sprintf("%s/32, %s/128", v4, v6)
	}

	confContent := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s
DNS = 2606:4700:4700::1111, 1.1.1.1
MTU = 1280

[Peer]
PublicKey = %s
Endpoint = %s
AllowedIPs = ::/0, 0.0.0.0/0
PersistentKeepalive = 25

[Resolve]
ResolveStrategy = ipv6

[Socks5]
BindAddress = 127.0.0.1:%d
`, dev.PrivateKey, addrLine, peerPub, endpoint, port)

	confPath := filepath.Join(store.DataDir(), fmt.Sprintf("wp_%d.conf", port))
	if err := os.WriteFile(confPath, []byte(confContent), 0o600); err != nil {
		return nil, fmt.Errorf("write wireproxy config: %w", err)
	}

	t0 := time.Now()
	cmd := exec.Command(binPath, "-s", "-c", confPath)
	if err := cmd.Start(); err != nil {
		_ = os.Remove(confPath)
		return nil, fmt.Errorf("launch wireproxy: %w", err)
	}
	bindChildToParentJob(cmd.Process)

	cleanup := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
		_ = os.Remove(confPath)
	}

	socksAddr := fmt.Sprintf("127.0.0.1:%d", port)
	if !waitForLocalPort(ctx, socksAddr, 3000*time.Millisecond) {
		cleanup()
		return nil, fmt.Errorf("wireproxy socks port %s did not open in time", socksAddr)
	}

	// Short settle window followed by adaptive trace probing (avoids fixed 900ms blind sleep).
	select {
	case <-time.After(200 * time.Millisecond):
	case <-ctx.Done():
		cleanup()
		return nil, ctx.Err()
	}

	trace, err := probeWarpTraceWithRetry(ctx, socksAddr, 3500*time.Millisecond)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("trace via %s (%s): %w", socksAddr, endpoint, err)
	}
	trace.Endpoint = endpoint
	trace.LocalPort = port
	trace.ProxyURL = "socks5://" + socksAddr
	trace.HandshakeMs = time.Since(t0).Milliseconds()

	if trace.WarpStatus != "on" && trace.WarpStatus != "plus" {
		cleanup()
		return nil, fmt.Errorf("tunnel trace warp=%q (expected on)", trace.WarpStatus)
	}
	if trace.Prefix64 == "" {
		cleanup()
		return nil, fmt.Errorf("tunnel trace egress IP %q is not IPv6", trace.IP)
	}

	return &WarpTunnelInstance{
		Trace:       trace,
		ConnectedAt: time.Now(),
		tlsClient:   createTLSClientForProxy(trace.ProxyURL, cfg.UpstreamTimeoutSeconds),
		stdClient:   createClientForProxy(trace.ProxyURL),
		stopFn:      cleanup,
	}, nil
}

var (
	portAllocMu sync.Mutex
	recentPorts = map[int]time.Time{}
)

func allocateFreeLocalPort() (int, error) {
	portAllocMu.Lock()
	defer portAllocMu.Unlock()

	now := time.Now()
	for p, exp := range recentPorts {
		if now.After(exp) {
			delete(recentPorts, p)
		}
	}

	var kept []net.Listener
	defer func() {
		for _, ln := range kept {
			_ = ln.Close()
		}
	}()

	for attempt := 0; attempt < 16; attempt++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return 0, err
		}
		addr, ok := ln.Addr().(*net.TCPAddr)
		if !ok || addr.Port <= 0 {
			_ = ln.Close()
			return 0, fmt.Errorf("unexpected tcp addr")
		}
		if exp, used := recentPorts[addr.Port]; used && now.Before(exp) {
			kept = append(kept, ln)
			continue
		}
		recentPorts[addr.Port] = now.Add(5 * time.Second)
		_ = ln.Close()
		return addr.Port, nil
	}
	return 0, fmt.Errorf("unable to allocate unique ephemeral port")
}

func waitForLocalPort(ctx context.Context, addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		default:
		}
		conn, err := net.DialTimeout("tcp", addr, 150*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		select {
		case <-time.After(40 * time.Millisecond):
		case <-ctx.Done():
			return false
		}
	}
	return false
}

func probeWarpTraceWithRetry(ctx context.Context, socksAddr string, totalBudget time.Duration) (WarpTraceInfo, error) {
	deadline := time.Now().Add(totalBudget)
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		rem := time.Until(deadline)
		if rem <= 200*time.Millisecond {
			break
		}
		perAttempt := 1600 * time.Millisecond
		if rem < perAttempt {
			perAttempt = rem
		}
		info, err := probeWarpTrace(ctx, socksAddr, perAttempt)
		if err == nil && info.IP != "" {
			return info, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return WarpTraceInfo{}, ctx.Err()
		}
		if attempt < 2 && time.Until(deadline) > 300*time.Millisecond {
			select {
			case <-time.After(200 * time.Millisecond):
			case <-ctx.Done():
				return WarpTraceInfo{}, ctx.Err()
			}
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("trace deadline exceeded")
	}
	return WarpTraceInfo{}, lastErr
}

func probeWarpTrace(ctx context.Context, socksAddr string, timeout time.Duration) (WarpTraceInfo, error) {
	var info WarpTraceInfo
	dialer, err := xproxy.SOCKS5("tcp", socksAddr, nil, xproxy.Direct)
	if err != nil {
		return info, err
	}
	tr := &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(_ context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		},
	}
	tCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(tCtx, http.MethodGet, "https://www.cloudflare.com/cdn-cgi/trace", nil)
	if err != nil {
		return info, err
	}
	client := &http.Client{Transport: tr, Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return info, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return info, err
	}

	for _, line := range strings.Split(string(raw), "\n") {
		kv := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "ip":
			info.IP = kv[1]
			if p64, ok := ExtractIPv6Prefix64(kv[1]); ok {
				info.Prefix64 = p64
			}
		case "colo":
			info.Colo = kv[1]
		case "loc":
			info.Loc = kv[1]
		case "warp":
			info.WarpStatus = kv[1]
		}
	}
	return info, nil
}

func downloadWireproxyRelease(ctx context.Context, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/windtf/wireproxy/releases/latest", nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "opencode-proxy")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var rel struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return err
	}

	goos := strings.ToLower(runtime.GOOS)
	goarch := strings.ToLower(runtime.GOARCH)
	var dlURL, assetName string
	for _, a := range rel.Assets {
		lower := strings.ToLower(a.Name)
		if strings.Contains(lower, goos) && strings.Contains(lower, goarch) &&
			(strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".zip")) {
			dlURL = a.BrowserDownloadURL
			assetName = a.Name
			break
		}
	}
	if dlURL == "" {
		return fmt.Errorf("no wireproxy asset found for %s/%s in %s", goos, goarch, rel.TagName)
	}

	dlReq, err := http.NewRequestWithContext(ctx, http.MethodGet, dlURL, nil)
	if err != nil {
		return err
	}
	dlReq.Header.Set("User-Agent", "opencode-proxy")
	dlResp, err := client.Do(dlReq)
	if err != nil {
		return err
	}
	defer dlResp.Body.Close()
	if dlResp.StatusCode != http.StatusOK {
		return fmt.Errorf("download asset HTTP %d", dlResp.StatusCode)
	}
	archiveBytes, err := io.ReadAll(io.LimitReader(dlResp.Body, 50<<20))
	if err != nil {
		return err
	}

	targetBase := "wireproxy"
	if runtime.GOOS == "windows" {
		targetBase = "wireproxy.exe"
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(destPath), ".wireproxy-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	extracted := false
	if strings.HasSuffix(strings.ToLower(assetName), ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
		if err != nil {
			_ = tmpFile.Close()
			return err
		}
		for _, f := range zr.File {
			if strings.EqualFold(filepath.Base(f.Name), targetBase) {
				rc, err := f.Open()
				if err != nil {
					_ = tmpFile.Close()
					return err
				}
				_, copyErr := io.Copy(tmpFile, rc)
				_ = rc.Close()
				if copyErr != nil {
					_ = tmpFile.Close()
					return copyErr
				}
				extracted = true
				break
			}
		}
	} else {
		gzr, err := gzip.NewReader(bytes.NewReader(archiveBytes))
		if err != nil {
			_ = tmpFile.Close()
			return err
		}
		tr := tar.NewReader(gzr)
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				_ = gzr.Close()
				_ = tmpFile.Close()
				return err
			}
			if strings.EqualFold(filepath.Base(hdr.Name), targetBase) {
				if _, err := io.Copy(tmpFile, tr); err != nil {
					_ = gzr.Close()
					_ = tmpFile.Close()
					return err
				}
				extracted = true
				break
			}
		}
		_ = gzr.Close()
	}

	if !extracted {
		_ = tmpFile.Close()
		return fmt.Errorf("%s not found in archive %s", targetBase, assetName)
	}
	_ = tmpFile.Chmod(0o755)
	if err := tmpFile.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, destPath)
}
