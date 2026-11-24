@echo off
chcp 65001 >nul
cd /d "%~dp0.."
where go >nul 2>nul
if errorlevel 1 (
  echo [错误] 未检测到 Go，请安装 Go 1.23 或更高版本。
  pause
  exit /b 1
)
if not exist data mkdir data
if not exist build mkdir build
echo 正在编译 GoHome...
go build -trimpath -ldflags="-s -w -X gohome/internal/app.version=0.2.11" -o build\gohome.exe ./cmd/gohome
if errorlevel 1 (
  echo [错误] 编译失败。
  pause
  exit /b 1
)
echo 浏览器访问：http://127.0.0.1:17880
build\gohome.exe
pause
