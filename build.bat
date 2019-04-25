@echo off
rem SPDX-License-Identifier: MIT
rem Copyright (C) 2019 WireGuard LLC. All Rights Reserved.

set STARTDIR=%cd%
set OLDPATH=%PATH%

if exist .deps\prepared goto :build
:installdeps
	rmdir /s /q .deps 2> NUL
	mkdir .deps || goto :error
	cd .deps || goto :error
	echo [+] Downloading golang
	curl -#fo go.zip https://dl.google.com/go/go1.12.3.windows-amd64.zip || goto :error
	echo [+] Verifying golang
	for /f %%a in ('CertUtil -hashfile go.zip SHA256 ^| findstr /r "^[0-9a-f]*$"') do if not "%%a"=="1806e089e85b84f192d782a7f70f90a32e0eccfd181405857e612f806ec04059" goto :error
	echo [+] Downloading mingw
	rem Mirror of https://musl.cc/x86_64-w64-mingw32-native.zip
	curl -#fo mingw.zip https://download.wireguard.com/windows-toolchain/distfiles/x86_64-w64-mingw32-native-20190307.zip || goto :error
	echo [+] Verifying mingw
	for /f %%a in ('CertUtil -hashfile mingw.zip SHA256 ^| findstr /r "^[0-9a-f]*$"') do if not "%%a"=="5390762183e181804b28eb13815b6210f85a1280057b815f749b06768215f817" goto :error
	echo [+] Extracting golang
	tar -xf go.zip || goto :error
	echo [+] Extracting mingw
	tar -xf mingw.zip || goto :error
	echo [+] Cleaning up
	del go.zip mingw.zip || goto :error
	copy /y NUL prepared > NUL || goto :error
	cd .. || goto :error

:build
	set PATH=%STARTDIR%\.deps\x86_64-w64-mingw32-native\bin\;%STARTDIR%\.deps\go\bin\;%PATH%
	set CC=x86_64-w64-mingw32-gcc.exe
	set CFLAGS=-O3 -Wall -std=gnu11
	set GOOS=windows
	set GOARCH=amd64
	set GOPATH=%STARTDIR%\.deps\gopath
	set GOROOT=%STARTDIR%\.deps\go
	set CGO_ENABLED=1
	echo [+] Assembling resources
	windres.exe -i resources.rc -o resources.syso -O coff || goto :error
	echo [+] Building program
	go build -ldflags="-H windowsgui -s -w" -v -o wireguard.exe || goto :error

:sign
	if exist .\sign.bat call .\sign.bat
	if "%SigningCertificate%"=="" goto :success
	if "%TimestampServer%"=="" goto :success
	echo [+] Signing
	signtool.exe sign /sha1 "%SigningCertificate%" /fd sha256 /tr "%TimestampServer%" /td sha256 /d WireGuard wireguard.exe || goto :error

:success
	echo [+] Success. Launch wireguard.exe.

:out
	set PATH=%OLDPATH%
	cd %STARTDIR%
	exit /b %errorlevel%

:error
	echo [-] Failed with error #%errorlevel%.
	goto :out
