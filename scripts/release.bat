@echo off
rem Cut a release on Windows. With no arg, auto-increments the patch number in
rem VERSION; with an arg, uses that exact version. Writes VERSION, commits,
rem pushes, then tags and pushes -- the tag push triggers the GitHub workflow
rem that builds and publishes every platform.
rem
rem Usage: release.bat [X.Y.Z]
setlocal enabledelayedexpansion

set "NV=%~1"
if "%NV%"=="" (
  set /p CUR=<VERSION
  for /f "tokens=1,2,3 delims=." %%a in ("!CUR!") do (
    set /a NP=%%c+1
    set "NV=%%a.%%b.!NP!"
  )
)

> VERSION echo !NV!
git add VERSION || exit /b 1
git commit -m "chore: release v!NV!" || exit /b 1
git push origin HEAD || exit /b 1
git tag -a "v!NV!" -m "Release v!NV!" || exit /b 1
git push origin "v!NV!" || exit /b 1
echo Released v!NV!. GitHub will build and publish all platforms.
