@echo off
cd /d C:\git\nps\cmd\npc\npc-dll

REM 统一 64 位工具链
set CGO_ENABLED=1
set CC=C:\mingw64\bin\x86_64-w64-mingw32-gcc.exe
set CXX=C:\mingw64\bin\x86_64-w64-mingw32-g++.exe
set PATH=C:\mingw64\bin;%PATH%

REM （可选）固定目标架构，防止环境污染
set GOOS=windows
set GOARCH=amd64

del /f /q npc_sdk.dll 2>nul

REM 更彻底清缓存（可选）
go clean -cache -testcache

REM 打详细日志并保留工作目录
go build -x -work -tags sdk -a -buildmode=c-shared -o npc_sdk.dll . 1>build.log 2>&1

notepad build.log

