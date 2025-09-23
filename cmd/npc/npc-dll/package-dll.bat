cd /d C:\git\nps\cmd\npc\npc-dll

set CGO_ENABLED=1
set CC=C:\mingw64\bin\x86_64-w64-mingw32-gcc.exe
set CXX=C:\mingw64\bin\x86_64-w64-mingw32-g++.exe
set AR=C:\mingw64\bin\ar.exe
set LD=C:\mingw64\bin\ld.exe
set RANLIB=C:\mingw64\bin\ranlib.exe
set PATH=C:\mingw64\bin;%PATH%

rem 让 cgo 把编译/链接命令都打印出来
set CGO_CFLAGS=-v
set CGO_LDFLAGS=-v

del npc_sdk.dll 2>nul
go clean -cache -testcache

rem 关键：-x -work 会打印并保留临时目录
go build -x -work -a -buildmode=c-shared -o npc_sdk.dll . 1>build.log 2>&1

notepad build.log


pause
