@echo off
echo Testing DLL build...

REM Clean old files
if exist npc-sdk.dll del npc-sdk.dll
if exist npc-sdk.h del npc-sdk.h

REM Set environment
set CGO_ENABLED=1
set GOOS=windows
set GOARCH=amd64
set CC=gcc

REM Build
go build -buildmode=c-shared -ldflags="-s -w" -o npc-sdk.dll sdk.go

REM Check result
if exist npc-sdk.dll (
    if exist npc-sdk.h (
        echo SUCCESS: Both DLL and header file created
        dir npc-sdk.*
    ) else (
        echo ERROR: DLL created but no header file
    )
) else (
    echo ERROR: Build failed
)