@echo off
cd /d C:\git\nps\cmd\npc\npc-dll

rem —— 确保 cgo 与 64 位 mingw 工具链启用 ——
set CGO_ENABLED=1
set CC=C:\mingw64\bin\x86_64-w64-mingw32-gcc.exe
set CXX=C:\mingw64\bin\x86_64-w64-mingw32-g++.exe
set PATH=C:\mingw64\bin;%PATH%
set GOOS=windows
set GOARCH=amd64

del /f /q npc_sdk.dll 2>nul
go clean -cache -testcache

rem 打印详细命令并保留临时目录
go build -x -work -tags sdk -a -buildmode=c-shared -o npc_sdk.dll . 1>build.log 2>&1

notepad build.log
