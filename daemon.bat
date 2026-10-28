@echo off
setlocal
title OpenCode Proxy - Daemon Manager
cd /d "%~dp0"

:: Cek apakah dijalankan dari dalam ZIP tanpa diekstrak
if not exist "%~dp0daemon.ps1" goto missing_files

:: Jika dijalankan dari CLI dengan argumen (contoh: daemon.bat start, daemon.bat stop, daemon.bat run)
if "%~1"=="" goto menu

powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0daemon.ps1" %*
if errorlevel 1 (
    echo.
    echo [ERROR] Perintah selesai dengan error code %errorlevel%.
    pause
)
exit /b %errorlevel%

:: Mode Interaktif (ketika di-double click dari Windows Explorer tanpa argumen)
:menu
cls
echo ======================================================================
echo                  OPENCODE PROXY DAEMON MANAGER
echo ======================================================================
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0daemon.ps1" status
echo ======================================================================
echo  PILIHAN MENU:
echo    [1] Start Daemon        - Jalankan proxy di background
echo    [2] Stop Daemon         - Hentikan background proxy
echo    [3] Restart Daemon      - Restart background proxy
echo    [4] Run Foreground      - Jalankan di terminal ini (live streaming log)
echo    [5] Stream Logs         - Pantau log background (real-time follow)
echo    [6] Refresh Status      - Perbarui status koneksi dan port
echo    [7] Install Autostart   - Aktifkan autostart saat login Windows
echo    [8] Uninstall Autostart - Hapus autostart dari Windows
echo    [0] Keluar (Exit)
echo ======================================================================
set "CHOICE="
set /p "CHOICE=Masukkan pilihan [0-8] (Default: 1): "

if not defined CHOICE set "CHOICE=1"
for /f "tokens=1" %%a in ("%CHOICE%") do set "CHOICE=%%a"

if /i "%CHOICE%"=="start" set "CHOICE=1"
if /i "%CHOICE%"=="stop" set "CHOICE=2"
if /i "%CHOICE%"=="restart" set "CHOICE=3"
if /i "%CHOICE%"=="run" set "CHOICE=4"
if /i "%CHOICE%"=="logs" set "CHOICE=5"
if /i "%CHOICE%"=="log" set "CHOICE=5"
if /i "%CHOICE%"=="status" set "CHOICE=6"
if /i "%CHOICE%"=="install" set "CHOICE=7"
if /i "%CHOICE%"=="uninstall" set "CHOICE=8"
if /i "%CHOICE%"=="exit" set "CHOICE=0"
if /i "%CHOICE%"=="q" set "CHOICE=0"

if "%CHOICE%"=="1" goto opt_start
if "%CHOICE%"=="2" goto opt_stop
if "%CHOICE%"=="3" goto opt_restart
if "%CHOICE%"=="4" goto opt_run
if "%CHOICE%"=="5" goto opt_logs
if "%CHOICE%"=="6" goto menu
if "%CHOICE%"=="7" goto opt_install
if "%CHOICE%"=="8" goto opt_uninstall
if "%CHOICE%"=="0" exit /b 0

echo.
echo Pilihan tidak dikenali: "%CHOICE%"
ping 127.0.0.1 -n 3 >nul
goto menu

:opt_start
echo.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0daemon.ps1" start
echo.
echo Tekan tombol apa saja untuk kembali ke menu...
pause >nul
goto menu

:opt_stop
echo.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0daemon.ps1" stop
echo.
echo Tekan tombol apa saja untuk kembali ke menu...
pause >nul
goto menu

:opt_restart
echo.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0daemon.ps1" restart
echo.
echo Tekan tombol apa saja untuk kembali ke menu...
pause >nul
goto menu

:opt_run
echo.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0daemon.ps1" run
echo.
echo Proxy foreground dihentikan.
echo Tekan tombol apa saja untuk kembali ke menu...
pause >nul
goto menu

:opt_logs
echo.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0daemon.ps1" logs -Follow
echo.
echo Tekan tombol apa saja untuk kembali ke menu...
pause >nul
goto menu

:opt_install
echo.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0daemon.ps1" install
echo.
echo Tekan tombol apa saja untuk kembali ke menu...
pause >nul
goto menu

:opt_uninstall
echo.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0daemon.ps1" uninstall
echo.
echo Tekan tombol apa saja untuk kembali ke menu...
pause >nul
goto menu

:missing_files
echo.
echo ======================================================================
echo  [PERINGATAN] File daemon.ps1 tidak ditemukan!
echo ======================================================================
echo  Anda menjalankan daemon.bat langsung dari dalam ZIP (Compressed Folder).
echo.
echo  Solusi:
echo  1. Jangan klik "Run" langsung dari dalam file .ZIP.
echo  2. Buka folder yang SUDAH DIEKSTRAK:
echo     C:\D\Project\Mix\opencode_proxy
echo  3. Jalankan daemon.bat dari folder tersebut.
echo ======================================================================
echo.
pause
exit /b 1
