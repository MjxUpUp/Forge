@echo off
REM Forge launcher shim for reasonix plugin hooks.
REM
REM reasonix resolves the hook command's first token ("forge") relative to the plugin directory
REM (and prepends the plugin dir to PATH), so a shim here is REQUIRED to reach the real forge
REM binary elsewhere on PATH — without it reasonix logs "command not found" and no hook fires.
REM Recursion guard: `where forge` lists this shim first (plugin dir is prepended to PATH), so we
REM skip any match in this shim's own directory and take the first real forge instead. Static
REM plugin asset (like install.sh / install.ps1), NOT generator output — forge plugin pack does
REM not touch it.
set "FORGE_BIN="
for /f "delims=" %%P in ('where forge 2^>nul') do if /I not "%%~dpP"=="%~dp0" if not defined FORGE_BIN set "FORGE_BIN=%%P"
if not defined FORGE_BIN (echo forge: not found on PATH outside plugin dir 1>&2 & exit /b 127)
call "%FORGE_BIN%" %*
exit /b %errorlevel%
