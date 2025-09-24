@echo off
chcp 65001 >nul
echo ========================================
echo Building NPC SDK DLL with TDM-GCC
echo ========================================

REM Check if TDM-GCC is installed
where gcc >nul 2>&1
if %errorlevel% neq 0 (
    echo Error: TDM-GCC not found in PATH
    echo Please install TDM-GCC and add it to your PATH
    echo Download from: https://jmeubank.github.io/tdm-gcc/
    pause
    exit /b 1
)

REM Show GCC version
echo Using GCC version:
gcc --version | findstr gcc

REM Set environment variables
set CGO_ENABLED=1
set GOOS=windows
set GOARCH=amd64
set CC=gcc

REM Clean previous build files
echo.
echo Cleaning previous build files...
if exist npc-sdk.dll del npc-sdk.dll
if exist npc-sdk.h del npc-sdk.h

REM Build DLL
echo.
echo Building DLL...
go build -buildmode=c-shared -ldflags="-s -w" -o npc-sdk.dll sdk.go

REM Wait a moment for file system operations to complete
timeout /t 1 /nobreak >nul 2>&1

REM Check build result
if exist npc-sdk.dll (
    if exist npc-sdk.h (
        echo.
        echo ========================================
        echo Build successful!
        echo ========================================
        echo Output files:
        echo   - npc-sdk.dll (64-bit DLL for C# interop)
        echo   - npc-sdk.h   (C header file)
        echo.
        echo File sizes:
        dir npc-sdk.dll npc-sdk.h
        echo.
        echo You can now use npc-sdk.dll in your C# project
        goto :success
    ) else (
        echo.
        echo ========================================
        echo Build partially failed!
        echo ========================================
        echo DLL created but header file missing
        pause
        exit /b 1
    )
) else (
    echo.
    echo ========================================
    echo Build failed!
    echo ========================================
    echo Please check the error messages above
    pause
    exit /b 1
)

:success
echo.
echo ========================================
echo Build completed successfully!
echo ========================================

echo.
echo Press any key to exit...
pause >nul