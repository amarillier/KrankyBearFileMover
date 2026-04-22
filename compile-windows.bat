@echo off
REM Windows batch wrapper for compile-windows.ps1 (invoked via SSH or locally).
REM PowerShell 5+ parses UTF-8 .ps1 with LF line endings; no in-place rewrite needed.

cd /d "%~dp0"

powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0compile-windows.ps1" %*

REM "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
