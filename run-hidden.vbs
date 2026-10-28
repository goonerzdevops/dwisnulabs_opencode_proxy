Set WshShell = CreateObject("WScript.Shell")
WshShell.CurrentDirectory = "C:\D\Project\Mix\opencode_proxy"
WshShell.Run "cmd.exe /c bin\opencode-proxy.exe -config C:\D\Project\Mix\opencode_proxy\config.json >> logs\opencode-proxy.log 2>&1", 0, False
