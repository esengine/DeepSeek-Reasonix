@ECHO OFF

IF "x86" == "%DOWNLOADPLATFORM%" (
	CALL c:\Windows\Microsoft.NET\Framework\v4.0.30319\RegAsm.exe /codebase /nologo c:\gopath\src\github.com\go-ole\go-ole\TestCOMServer.dll
)
IF "x64" == "%DOWNLOADPLATFORM%" (
	CALL c:\Windows\Microsoft.NET\Framework64\v4.0.30319\RegAsm.exe /codebase /nologo c:\gopath\src\github.com\go-ole\go-ole\TestCOMServer.dll
)
