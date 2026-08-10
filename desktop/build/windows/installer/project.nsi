Unicode true

####
## Reasonix per-user NSIS installer.
##
## This file is COMMITTED and customized (Wails leaves an existing project.nsi
## untouched and only regenerates wails_tools.nsh). The customizations vs.
## Wails' default template:
##
##   1. REQUEST_EXECUTION_LEVEL "user" + InstallDir under $LOCALAPPDATA - install
##      without administrator rights. This lets the auto-updater re-run a freshly
##      downloaded installer in a visible progress-only mode with no UAC prompt.
##   2. Uninstall registry under HKCU (not HKLM). Wails' wails.writeUninstaller /
##      wails.deleteUninstaller macros hard-code HKLM, which a non-admin install
##      cannot write - so we inline HKCU versions below instead.
##   3. InstallDir is remembered across updates via InstallDirRegKey +
##      InstallLocation (HKCU\...\Uninstall\InstallLocation). When upgrading from
##      a build that did not write InstallLocation yet, .onInit falls back to the
##      old DisplayIcon path before using the default. Without this, every release
##      forces the user back to %LOCALAPPDATA%\Programs\Reasonix even if they had
##      moved the install to a different drive (e.g. D:\Tools\Reasonix); the
##      auto-updater would overwrite the wrong dir, leaving the old install
##      orphaned.
##
## Everything else mirrors Wails' generated default. Defines below override the
## ProjectInfo values that wails_tools.nsh would otherwise populate.
####

## Install per-user (no admin). Must be defined BEFORE including wails_tools.nsh,
## which only sets the "admin" default when REQUEST_EXECUTION_LEVEL is undefined.
!define REQUEST_EXECUTION_LEVEL "user"

####
## Include the wails tools (auto-generated; provides INFO_* defines and the
## wails.* macros used below).
####
!include "wails_tools.nsh"
!include "FileFunc.nsh"
!include "LogicLib.nsh"

# The build script writes this host-specific include before invoking makensis.
# Keep a Windows fallback so opening this script directly still behaves like a
# native Windows build.
!if /FileExists "reasonix_host.nsh"
!include "reasonix_host.nsh"
!endif
!ifndef REASONIX_UNINST_FINALIZE
!define REASONIX_UNINST_FINALIZE 'cmd.exe /C copy /Y "%1" "reasonix-uninstall.exe" >NUL'
!endif

# The version information for this two must consist of 4 parts
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

# Enable HiDPI support. https://nsis.sourceforge.io/Reference/ManifestDPIAware
ManifestDPIAware true

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
# !define MUI_WELCOMEFINISHPAGE_BITMAP "resources\leftimage.bmp" #Include this to add a bitmap on the left side of the Welcome Page. Must be a size of 164x314
!define MUI_FINISHPAGE_NOAUTOCLOSE # Wait on the INSTFILES page so the user can take a look into the details of the installation steps
!define MUI_ABORTWARNING # This will warn the user if they exit from the installer.

!define MUI_PAGE_CUSTOMFUNCTION_PRE reasonix.skipSetupPageForUpdate
!insertmacro MUI_PAGE_WELCOME # Welcome to the installer page.
# !insertmacro MUI_PAGE_LICENSE "resources\eula.txt" # Adds a EULA page to the installer
!define MUI_PAGE_CUSTOMFUNCTION_PRE reasonix.skipSetupPageForUpdate
!insertmacro MUI_PAGE_DIRECTORY # In which folder install page.
!define MUI_PAGE_CUSTOMFUNCTION_SHOW reasonix.showUpdateProgress
!insertmacro MUI_PAGE_INSTFILES # Installing page.
!define MUI_PAGE_CUSTOMFUNCTION_PRE reasonix.skipFinishPageForUpdate
!insertmacro MUI_PAGE_FINISH # Finished installation page.

!insertmacro MUI_UNPAGE_INSTFILES # Uinstalling page

!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "SimpChinese"
!insertmacro MUI_LANGUAGE "TradChinese"

LangString reasonixUpdateTitle ${LANG_ENGLISH} "Updating Reasonix"
LangString reasonixUpdateTitle ${LANG_SIMPCHINESE} "正在更新 Reasonix"
LangString reasonixUpdateTitle ${LANG_TRADCHINESE} "正在更新 Reasonix"
LangString reasonixUpdateSubtitle ${LANG_ENGLISH} "Installing the verified update. Reasonix will restart automatically."
LangString reasonixUpdateSubtitle ${LANG_SIMPCHINESE} "正在安装已验证的更新，完成后 Reasonix 将自动重启。"
LangString reasonixUpdateSubtitle ${LANG_TRADCHINESE} "正在安裝已驗證的更新，完成後 Reasonix 將自動重新啟動。"

## Preserve the first-pass generated uninstaller so the release workflow can
## Authenticode-sign it together with the other installed payload files.
## The second pass provides ARG_REASONIX_SIGNED_UNINSTALLER and embeds that
## signed binary instead of generating another unsigned uninstaller.
!ifndef ARG_REASONIX_SIGNED_UNINSTALLER
!uninstfinalize '${REASONIX_UNINST_FINALIZE}'
!endif
#!finalize 'signtool --file "%1"'

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe" # Name of the installer's file.
!define REASONIX_DEFAULT_INSTALLDIR "$LOCALAPPDATA\Programs\${INFO_PRODUCTNAME}"
!define REASONIX_UPDATE_HELPER "reasonix-update-helper.exe"
!define REASONIX_GUARD "reasonix-guard.exe"
!define REASONIX_LAUNCHER "reasonix-launcher.exe"
!define REASONIX_CLI "reasonix-cli.exe"
!define REASONIX_PORTABLE_ENTRY "Reasonix.exe"
!define REASONIX_LAYOUT_INSTALLER "reasonix-layout-installer.exe"
!define REASONIX_PAYLOAD_MANIFEST "reasonix-payload.json"
!define REASONIX_PAYLOAD_SIGNATURE "reasonix-payload.json.minisig"
!define REASONIX_LEGACY_UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\Reasonix"
!define REASONIX_LEGACY_PRODUCT_KEY "Software\reasonix\Reasonix"
!define REASONIX_UNLOCK_RETRIES 60
Var ReasonixUpdateMode
Var ReasonixStageMode
InstallDirRegKey HKCU "${UNINST_KEY}" "InstallLocation" # Reuse the previous install path on update; .onInit falls back to the default on first install.
InstallDir "${REASONIX_DEFAULT_INSTALLDIR}" # Per-user install location (no admin rights required).
ShowInstDetails show # This will always show the installation details.

####
## Per-user uninstaller registry (HKCU). Replaces wails.writeUninstaller /
## wails.deleteUninstaller, which write HKLM and would fail without admin rights.
####
!macro reasonix.writeUninstaller
    !ifdef ARG_REASONIX_SIGNED_UNINSTALLER
    File "/oname=uninstall.exe" "${ARG_REASONIX_SIGNED_UNINSTALLER}"
    !else
    WriteUninstaller "$INSTDIR\uninstall.exe"
    !endif

    WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "${INFO_COMPANYNAME}"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "${INFO_PRODUCTNAME}"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${INFO_PRODUCTVERSION}"
    !if /FileExists "${REASONIX_LAUNCHER}"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${REASONIX_LAUNCHER}"
    !else
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    !endif
    WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
    WriteRegStr HKCU "${UNINST_KEY}" "QuietUninstallString" "$\"$INSTDIR\uninstall.exe$\" /S"
    # Persist the resolved install path so a subsequent update picks it up
    # via InstallDirRegKey above. Without this, every release would force the
    # user back to %LOCALAPPDATA%\Programs\Reasonix even if they had moved
    # the install to a different drive (e.g. D:\Tools\Reasonix). The auto-
    # updater trusts this persisted path, so it has to be present before the
    # visible progress-only re-install.
    WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"

    ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
    IntFmt $0 "0x%08X" $0
    WriteRegDWORD HKCU "${UNINST_KEY}" "EstimatedSize" "$0"
!macroend

; Tauri 0.53 separately persisted $INSTDIR under its manufacturer/product key
; and restores that value before every later install. Clear only a same-root
; value so re-running 0.53 cannot overwrite the current uninstaller; preserve a
; genuinely separate legacy installation. If this cleanup fails, retain the old
; uninstall alias so a later update can retry the migration.
!macro reasonix.deleteLegacyInstallerStateIfOwned
    StrCpy $1 "1"
    ClearErrors
    ReadRegStr $0 HKCU "${REASONIX_LEGACY_PRODUCT_KEY}" ""
    ${If} $0 == "$INSTDIR"
        ClearErrors
        DeleteRegValue HKCU "${REASONIX_LEGACY_PRODUCT_KEY}" ""
        ${If} ${Errors}
            StrCpy $1 "0"
        ${EndIf}
    ${ElseIf} $0 == "$\"$INSTDIR$\""
        ClearErrors
        DeleteRegValue HKCU "${REASONIX_LEGACY_PRODUCT_KEY}" ""
        ${If} ${Errors}
            StrCpy $1 "0"
        ${EndIf}
    ${EndIf}

    ${If} $1 == "1"
        ClearErrors
        ReadRegStr $0 HKCU "${REASONIX_LEGACY_UNINST_KEY}" "InstallLocation"
        ${If} $0 == "$INSTDIR"
            DeleteRegKey HKCU "${REASONIX_LEGACY_UNINST_KEY}"
        ${ElseIf} $0 == "$\"$INSTDIR$\""
            DeleteRegKey HKCU "${REASONIX_LEGACY_UNINST_KEY}"
        ${Else}
            ClearErrors
            ReadRegStr $0 HKCU "${REASONIX_LEGACY_UNINST_KEY}" "UninstallString"
            ${If} $0 == "$INSTDIR\uninstall.exe"
                DeleteRegKey HKCU "${REASONIX_LEGACY_UNINST_KEY}"
            ${ElseIf} $0 == "$\"$INSTDIR\uninstall.exe$\""
                DeleteRegKey HKCU "${REASONIX_LEGACY_UNINST_KEY}"
            ${EndIf}
        ${EndIf}
    ${EndIf}
!macroend

!macro reasonix.deleteUninstaller
    Delete "$INSTDIR\uninstall.exe"
    DeleteRegKey HKCU "${UNINST_KEY}"
!macroend

Function .onInit
   !insertmacro wails.checkArchitecture

   ; The helper passes /REASONIXUPDATE=1 and a final /D=<current directory>.
   ; This mode remains visible but skips every page that could change the
   ; destination, then closes automatically after the file copy so the helper
   ; can relaunch Reasonix. A normal manual installer keeps the full wizard.
   StrCpy $ReasonixUpdateMode "0"
   StrCpy $ReasonixStageMode "0"
   ${GetParameters} $R0
   ClearErrors
   ${GetOptions} $R0 "/REASONIXUPDATE=" $R1
   IfErrors reasonix_update_mode_done
   StrCmp $R1 "1" 0 reasonix_update_mode_done
   StrCpy $ReasonixUpdateMode "1"

reasonix_update_mode_done:
   ClearErrors
   ${GetOptions} $R0 "/REASONIXSTAGE=" $R2
   IfErrors reasonix_stage_mode_done
   StrCmp $R2 "1" 0 reasonix_stage_mode_done
   StrCpy $ReasonixStageMode "1"

reasonix_stage_mode_done:

   ; InstallDirRegKey leaves $INSTDIR empty when the InstallLocation value is
   ; missing. Older installers still wrote DisplayIcon, so use its parent folder
   ; as a compatibility bridge before falling back to the per-user default.
   StrCmp $INSTDIR "" 0 done
   ClearErrors
   ReadRegStr $0 HKCU "${UNINST_KEY}" "DisplayIcon"
   IfErrors legacy_location
   StrCmp $0 "" legacy_location
   ${GetParent} "$0" $INSTDIR
   StrCmp $INSTDIR "" legacy_location done

legacy_location:
   ; Tauri 0.53 used a different uninstall key and may have stored the selected
   ; directory with surrounding quotes (for example "D:\Reasonix"). Reuse it
   ; only while its uninstaller still exists so a stale registry value cannot
   ; redirect the repair installer into an unrelated directory.
   ClearErrors
   ReadRegStr $0 HKCU "${REASONIX_LEGACY_UNINST_KEY}" "InstallLocation"
   IfErrors legacy_uninstaller
   StrCmp $0 "" legacy_uninstaller
   StrCpy $1 $0 1
   StrCmp $1 "$\"" 0 legacy_location_ready
   StrCpy $1 $0 1 -1
   StrCmp $1 "$\"" 0 legacy_location_ready
   StrCpy $0 $0 -1 1

legacy_location_ready:
   IfFileExists "$0\uninstall.exe" 0 legacy_uninstaller
   StrCpy $INSTDIR $0
   Goto done

legacy_uninstaller:
   ClearErrors
   ReadRegStr $0 HKCU "${REASONIX_LEGACY_UNINST_KEY}" "UninstallString"
   IfErrors fallback
   StrCmp $0 "" fallback
   StrCpy $1 $0 1
   StrCmp $1 "$\"" 0 legacy_uninstaller_ready
   StrCpy $1 $0 1 -1
   StrCmp $1 "$\"" 0 legacy_uninstaller_ready
   StrCpy $0 $0 -1 1

legacy_uninstaller_ready:
   IfFileExists "$0" 0 fallback
   ${GetParent} "$0" $INSTDIR
   StrCmp $INSTDIR "" fallback done

fallback:
   StrCpy $INSTDIR "${REASONIX_DEFAULT_INSTALLDIR}"
done:
FunctionEnd

Function reasonix.skipSetupPageForUpdate
   StrCmp $ReasonixUpdateMode "1" 0 reasonix_show_setup_page
   Abort

reasonix_show_setup_page:
FunctionEnd

Function reasonix.showUpdateProgress
   StrCmp $ReasonixUpdateMode "1" 0 reasonix_update_progress_done
   !insertmacro MUI_HEADER_TEXT "$(reasonixUpdateTitle)" "$(reasonixUpdateSubtitle)"
   SetDetailsView hide
   SetAutoClose true
   BringToFront

reasonix_update_progress_done:
FunctionEnd

Function reasonix.skipFinishPageForUpdate
   StrCmp $ReasonixUpdateMode "1" 0 reasonix_show_finish_page
   Abort

reasonix_show_finish_page:
FunctionEnd

Function reasonix.waitForExecutableUnlock
   StrCpy $0 0

retry:
   IfFileExists "$INSTDIR\${PRODUCT_EXECUTABLE}" 0 check_versioned_target
   ClearErrors
   FileOpen $1 "$INSTDIR\${PRODUCT_EXECUTABLE}" a
   IfErrors locked
   FileClose $1

check_versioned_target:
   ; A same-version recovery install replaces this directory transactionally.
   ; Detect the running active binary before asking the Go activator to rename it.
   IfFileExists "$INSTDIR\versions\v${INFO_PRODUCTVERSION}\${PRODUCT_EXECUTABLE}" 0 check_guard
   ClearErrors
   FileOpen $1 "$INSTDIR\versions\v${INFO_PRODUCTVERSION}\${PRODUCT_EXECUTABLE}" a
   IfErrors locked
   FileClose $1

check_guard:
   IfFileExists "$INSTDIR\${REASONIX_GUARD}" 0 check_launcher
   ClearErrors
   FileOpen $1 "$INSTDIR\${REASONIX_GUARD}" a
   IfErrors locked
   FileClose $1

check_launcher:
	IfFileExists "$INSTDIR\${REASONIX_LAUNCHER}" 0 check_cli
	ClearErrors
	FileOpen $1 "$INSTDIR\${REASONIX_LAUNCHER}" a
	IfErrors locked
	FileClose $1

check_cli:
	IfFileExists "$INSTDIR\${REASONIX_CLI}" 0 check_portable_entry
	ClearErrors
	FileOpen $1 "$INSTDIR\${REASONIX_CLI}" a
	IfErrors locked
	FileClose $1

check_portable_entry:
   IfFileExists "$INSTDIR\${REASONIX_PORTABLE_ENTRY}" 0 done
   ClearErrors
   FileOpen $1 "$INSTDIR\${REASONIX_PORTABLE_ENTRY}" a
   IfErrors locked
   FileClose $1
   Goto done

locked:
   IntOp $0 $0 + 1
   IntCmp $0 ${REASONIX_UNLOCK_RETRIES} failed 0 0
   Sleep 1000
   Goto retry

failed:
   IfSilent silent interactive

interactive:
   MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "Reasonix is still running. Close Reasonix, then click Retry to continue the installation." IDRETRY retry IDCANCEL abort
   Goto retry

silent:
   SetErrorLevel 1618

abort:
   Abort "Reasonix is still running. Close Reasonix and run the installer again."

done:
FunctionEnd

Section
    !insertmacro wails.setShellContext

    ; /REASONIXSTAGE=1: flat six-member payload for 1.18–1.19.1 helpers (and
    ; the new helper's staging extract). Do not write shortcuts/uninstaller.
    ; Normal install: versioned-v1 layout under versions/v${INFO_PRODUCTVERSION}/
    ; with a permanent thin launcher at InstallRoot. Guard is only present in
    ; STAGE payloads (as the one-shot legacy migrator) and is not persisted on
    ; a normal install.
    StrCmp $ReasonixStageMode "1" reasonix_stage_payload
    !insertmacro wails.webview2runtime
    Call reasonix.waitForExecutableUnlock
    Goto reasonix_normal_install

reasonix_stage_payload:
    SetOutPath $INSTDIR
    !if /FileExists "${REASONIX_PAYLOAD_MANIFEST}"
    File "/oname=${REASONIX_PAYLOAD_MANIFEST}" "${REASONIX_PAYLOAD_MANIFEST}"
    !endif
    !if /FileExists "${REASONIX_PAYLOAD_SIGNATURE}"
    File "/oname=${REASONIX_PAYLOAD_SIGNATURE}" "${REASONIX_PAYLOAD_SIGNATURE}"
    !endif
    !insertmacro wails.files
    !if /FileExists "${REASONIX_UPDATE_HELPER}"
    File "/oname=${REASONIX_UPDATE_HELPER}" "${REASONIX_UPDATE_HELPER}"
    !endif
    !if /FileExists "${REASONIX_GUARD}"
    File "/oname=${REASONIX_GUARD}" "${REASONIX_GUARD}"
    !endif
    !if /FileExists "${REASONIX_LAUNCHER}"
    File "/oname=${REASONIX_LAUNCHER}" "${REASONIX_LAUNCHER}"
    !endif
    !if /FileExists "${REASONIX_CLI}"
    File "/oname=${REASONIX_CLI}" "${REASONIX_CLI}"
    !endif
    Goto reasonix_section_done

reasonix_normal_install:
    ; Extract into an install-local temporary directory, then let the signed Go
    ; activator validate the complete release unit, transactionally publish the
    ; version/root entries, and strictly atomically replace current.json last.
    ; The normal/recovery installer therefore shares the same commit protocol as
    ; automatic updates instead of writing live files or current.json in place.
    System::Call 'kernel32::GetCurrentProcessId() i .R8'
    CreateDirectory "$INSTDIR\versions"
    StrCpy $R9 "$INSTDIR\versions\.installer-v${INFO_PRODUCTVERSION}-$R8"
    RMDir /r "$R9"
    CreateDirectory "$R9"
    SetOutPath "$R9"
    !insertmacro wails.files
    !if /FileExists "${REASONIX_UPDATE_HELPER}"
    File "/oname=${REASONIX_UPDATE_HELPER}" "${REASONIX_UPDATE_HELPER}"
    !else
    !warning "${REASONIX_UPDATE_HELPER} was not found; Windows auto-update will fail safely until the helper is installed."
    !endif
    !if /FileExists "${REASONIX_CLI}"
    File "/oname=${REASONIX_CLI}" "${REASONIX_CLI}"
    !else
    !warning "${REASONIX_CLI} was not found; remote upload installation will be unavailable."
    !endif
    !if /FileExists "${REASONIX_LAUNCHER}"
    File "/oname=${REASONIX_LAUNCHER}" "${REASONIX_LAUNCHER}"
    !endif

    SetOutPath "$PLUGINSDIR"
    !if /FileExists "${REASONIX_GUARD}"
    File "/oname=${REASONIX_LAYOUT_INSTALLER}" "${REASONIX_GUARD}"
    !else
    !error "${REASONIX_GUARD} was not found; normal installs require the signed layout activator."
    !endif
    DetailPrint "Reasonix layout activator output:"
    nsExec::ExecToLog /OEM '"$PLUGINSDIR\${REASONIX_LAYOUT_INSTALLER}" --install-root "$INSTDIR" --version "v${INFO_PRODUCTVERSION}" --activate-staging "$R9" --no-relaunch'
    Pop $0
    StrCmp $0 "0" reasonix_layout_activated
    DetailPrint "Reasonix layout activation failed with exit code $0; the previous version remains active."
    RMDir /r "$R9"
    SetErrorLevel 1
    Abort "Reasonix could not activate the verified release. The previous version was left unchanged."

reasonix_layout_activated:
    RMDir /r "$R9"
    SetOutPath "$INSTDIR"

    ; Remove flat leftovers from prior 1.18–1.19 installs when overwriting.
    Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"
    Delete "$INSTDIR\${REASONIX_GUARD}"
    Delete "$INSTDIR\${REASONIX_UPDATE_HELPER}"

    !if /FileExists "${REASONIX_LAUNCHER}"
    ; Keep both target and icon on the stable launcher. Pointing IconLocation at
    ; versions\vX\reasonix-desktop.exe leaves a blank shortcut as soon as version
    ; retention removes that directory after a later update.
    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${REASONIX_LAUNCHER}" "" "$INSTDIR\${REASONIX_LAUNCHER}" 0
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${REASONIX_LAUNCHER}" "" "$INSTDIR\${REASONIX_LAUNCHER}" 0
    !else
    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\versions\v${INFO_PRODUCTVERSION}\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\versions\v${INFO_PRODUCTVERSION}\${PRODUCT_EXECUTABLE}"
    !endif

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols
    !insertmacro reasonix.writeUninstaller
    !insertmacro reasonix.deleteLegacyInstallerStateIfOwned

reasonix_section_done:
SectionEnd

Section "uninstall"
    !insertmacro wails.setShellContext

    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}" # Remove the WebView2 DataPath

    ; Precision uninstall: flat leftovers, thin entry points, and version trees.
    Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"
    Delete "$INSTDIR\${REASONIX_UPDATE_HELPER}"
    Delete "$INSTDIR\${REASONIX_GUARD}"
    Delete "$INSTDIR\${REASONIX_LAUNCHER}"
    Delete "$INSTDIR\${REASONIX_CLI}"
    Delete "$INSTDIR\${REASONIX_PORTABLE_ENTRY}"
    Delete "$INSTDIR\current.json"
    RMDir /r "$INSTDIR\versions"

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    !insertmacro reasonix.deleteUninstaller
    !insertmacro reasonix.deleteLegacyInstallerStateIfOwned

    ; Only remove the installation directory if it is empty to prevent data loss
    RMDir $INSTDIR
SectionEnd
