# electron-builder includes this file before its own script, so LogicLib is not
# loaded yet and anything outside a macro body would not compile. Its own guard
# makes the second include a no-op.
!include LogicLib.nsh

# common.nsh builds ${UNINSTALL_FILENAME} the same way, and cannot be included
# twice: its defines are unguarded. PRODUCT_FILENAME arrives on the command
# line, so the one thing needed from it is available this early.
!define RX_UNINSTALLER "Uninstall ${PRODUCT_FILENAME}.exe"

# Taking over the Wails install, once.
#
# It is per-machine under Program Files with an uninstall key of its own
# (studio.nsi writes Software\ReasonixStudio and ...\Uninstall\ReasonixStudio),
# while this one is per-user with a key electron-builder derives from the appId.
# Nothing about installing this one would ever replace that one: both would sit
# in Add/Remove Programs, both would stay on disk, and only one of them would
# ever be updated again.
#
# Its uninstaller manifests admin, so ExecShellWait rather than ExecWait --
# CreateProcess refuses that image from an installer running as the user. A
# declined prompt leaves the old install where it was and this one working: the
# takeover never fails the install it is cleaning up after.

# view is the registry view to look in. The Wails installer never called
# SetRegView, so on x64 it wrote through WOW64 redirection into
# Software\WOW6432Node; electron-builder has selected the 64-bit view by the
# time this runs, which is where that key is not.
!macro takeOverLegacyStudio view
  SetRegView ${view}
  ReadRegStr $R9 HKLM "Software\ReasonixStudio" "InstallDir"
  ${If} $R9 != ""
  ${AndIf} ${FileExists} "$R9\uninstall.exe"
    DetailPrint "Removing the previous Reasonix Studio installation"
    ExecShellWait "open" "$R9\uninstall.exe" "/S" SW_HIDE
  ${EndIf}
!macroend

# A register rather than a Var: this file is compiled into the uninstaller
# script as well, where customInstall is never inserted and a declared Var is
# an unused one -- which electron-builder builds with warnings as errors.
!macro customInstall
  Push $R9
  !insertmacro takeOverLegacyStudio 32
  !insertmacro takeOverLegacyStudio 64
  # Leave the view electron-builder chose, not the one looked in last.
  ${If} ${RunningX64}
    SetRegView 64
  ${Else}
    SetRegView 32
  ${EndIf}
  Pop $R9
!macroend

# An update runs the *previous* install's uninstaller, and that uninstaller
# takes $INSTDIR whole: every entry under it, not a record of what was
# installed. So a folder holding anything else loses it on the next update,
# silently, months after the folder was chosen. The choice is the only place
# that can still refuse, which is here.
#
# Our own previous install is exactly what an update replaces, so a folder
# carrying our uninstaller is the one folder that may already have things in it.
!macro instDirTakesNothingElse result
  Push $R0
  Push $R1
  StrCpy ${result} 1
  ${If} ${FileExists} "$INSTDIR\${RX_UNINSTALLER}"
  ${Else}
    FindFirst $R0 $R1 "$INSTDIR\*.*"
    ${DoWhile} $R1 != ""
      ${If} $R1 != "."
      ${AndIf} $R1 != ".."
        StrCpy ${result} 0
        ${ExitDo}
      ${EndIf}
      FindNext $R0 $R1
    ${Loop}
    FindClose $R0
  ${EndIf}
  Pop $R1
  Pop $R0
!macroend

!ifndef BUILD_UNINSTALLER
  # Greys out Next on the directory page. There is no way to say why from here,
  # so the page's own text carries the requirement.
  Function .onVerifyInstDir
    Push $R2
    !insertmacro instDirTakesNothingElse $R2
    ${If} $R2 == 0
      Pop $R2
      Abort
    ${EndIf}
    Pop $R2
  FunctionEnd
!endif

# A silent install never reaches the directory page, and /D= is how a folder
# full of someone's work gets named in the first place. Refuse with an exit
# code, because that is all a script driving this can read.
!macro customInit
  Push $R9
  !insertmacro instDirTakesNothingElse $R9
  ${If} $R9 == 0
    Pop $R9
    DetailPrint "Refusing to install into $INSTDIR: it already holds other files, and an update would remove the whole folder"
    MessageBox MB_ICONSTOP "Reasonix Studio installs into a folder of its own.$\r$\n$\r$\nAn update replaces the whole install folder, so anything else kept in $INSTDIR would be deleted with it. Choose an empty folder, or the default location." /SD IDOK
    SetErrorLevel 2
    Abort
  ${EndIf}
  Pop $R9
!macroend
