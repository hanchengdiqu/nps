@echo off
echo ========================================
echo Building NPC Client
echo ========================================

:: Set environment variables
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0

:: Show current directory
echo Current directory: %CD%

:: Check if npc.go file exists
if not exist "npc.go" (
    echo Error: npc.go file not found
    echo Please make sure to run this batch file in cmd\npc directory
    pause
    exit /b 1
)

:: Compile npc.exe
echo Compiling npc.exe...
go build -o npc.exe -gcflags "all=-N -l" npc.go

:: Check if compilation was successful
if %ERRORLEVEL% neq 0 (
    echo Compilation failed!
    pause
    exit /b 1
)

:: Check generated file
if exist "npc.exe" (
    echo Compilation successful!
    dir npc.exe | findstr "npc.exe"
    
    :: Copy to pack\npc directory
    echo Copying to pack\npc directory...
    if not exist "..\..\pack\npc" (
        mkdir "..\..\pack\npc"
    )
    copy "npc.exe" "..\..\pack\npc\npc.exe" >nul
    if %ERRORLEVEL% equ 0 (
        echo Successfully copied to pack\npc directory
    ) else (
        echo Failed to copy to pack\npc directory
    )
) else (
    echo Compilation failed: npc.exe file not found
    pause
    exit /b 1
)

echo ========================================
echo Build completed!
echo ========================================
pause