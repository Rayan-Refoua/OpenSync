@echo off
setlocal
where.exe python >nul 2>nul
if errorlevel 1 (
    py -3 "%~dp0tools\build_open_windows.py" --local %*
) else (
    python "%~dp0tools\build_open_windows.py" --local %*
)
if errorlevel 1 (
    echo.
    echo OpenSync did not build or launch. See the error above.
    pause
    exit /b 1
)
