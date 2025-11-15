@echo off
REM Windows batch wrapper for compile-windows.ps1
REM This can work better when invoked via SSH than calling PowerShell directly
REM Also fixes line endings (LF to CRLF) before execution

cd /d "%~dp0"

REM Fix line endings in the PowerShell script (convert LF to CRLF)
REM This is needed when files are synced from Unix systems
REM Read file as bytes, convert to string, normalize line endings, write back
powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "$f='%~dp0compile-windows.ps1';if(Test-Path $f){$b=[System.IO.File]::ReadAllBytes($f);$s=[System.Text.Encoding]::UTF8.GetString($b);$s=$s.Replace([char]10,[char]13+[char]10).Replace([string][char]13+[string][char]13+[string][char]10,[string][char]13+[string][char]10);[System.IO.File]::WriteAllText($f,$s,[System.Text.Encoding]::UTF8)}"

REM Now execute the PowerShell script
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0compile-windows.ps1" %*

REM "Now this is not even the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
