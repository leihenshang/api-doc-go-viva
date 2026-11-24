; api-doc-go-viva 的 Windows 安装程序（NSIS 3）。
;
; 与 Wails 版的关系：那边复用 wails 生成的 wails_tools.nsh 与 WebView2 引导器；
; 本产品是纯 Go 原生渲染，没有 WebView2、也没有需要 wails 生成的宏，所以这份脚本
; 完全自包含，只依赖 NSIS 自带的 MUI2 / LogicLib / x64 / FileFunc。
;
; 所有 !define 都可以由 scripts/build-win.sh 用 -D 覆盖（下面每个默认值都有
; !ifndef 守护），脚本只负责注入版本号、架构、安装范围与产物路径。
;
; 打包前置：
;   1) 交叉编译好的 exe 与 icon.ico 在 ${STAGE_DIR} 里
;   2) makensis 可用（Linux: 解出 deb 后 NSISDIR=... 指向 share/nsis）

Unicode true

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "x64.nsh"
!include "FileFunc.nsh"

; ---- 可由 -D 覆盖的变量 ----

!ifndef PRODUCT_NAME
  !define PRODUCT_NAME "api-doc-go-viva"
!endif
!ifndef COMPANY_NAME
  !define COMPANY_NAME "api-doc-go"
!endif
!ifndef APP_VERSION
  !define APP_VERSION "0.1.0"
!endif
; VIProductVersion 要四段纯数字，由脚本算好传进来
!ifndef APP_VERSION4
  !define APP_VERSION4 "${APP_VERSION}.0"
!endif
!ifndef APP_EXE
  !define APP_EXE "api-doc-viva.exe"
!endif
!ifndef APP_ARCH
  !define APP_ARCH "amd64"
!endif
; machine = Program Files（默认，需管理员）；user = %LOCALAPPDATA%\Programs（免提权）
!ifndef INSTALL_SCOPE
  !define INSTALL_SCOPE "machine"
!endif
!ifndef UNINST_KEY
  !define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\api-doc-go-viva"
!endif
!ifndef STAGE_DIR
  !define STAGE_DIR "stage"
!endif
!ifndef ICON_FILE
  !define ICON_FILE "icon.ico"
!endif
!ifndef OUT_FILE
  !define OUT_FILE "api-doc-go-viva-${APP_ARCH}-setup.exe"
!endif
!ifndef COPYRIGHT
  !define COPYRIGHT "Copyright (C) 2026 api-doc-go"
!endif

; ---- 版本信息（资源管理器「属性 → 详细信息」里能看到） ----

VIProductVersion "${APP_VERSION4}"
VIFileVersion    "${APP_VERSION4}"
VIAddVersionKey "CompanyName"     "${COMPANY_NAME}"
VIAddVersionKey "FileDescription" "${PRODUCT_NAME} 安装程序"
VIAddVersionKey "FileVersion"     "${APP_VERSION}"
VIAddVersionKey "ProductVersion"  "${APP_VERSION}"
VIAddVersionKey "LegalCopyright"  "${COPYRIGHT}"
VIAddVersionKey "ProductName"     "${PRODUCT_NAME}"

ManifestDPIAware true
Name "${PRODUCT_NAME} ${APP_VERSION}"
BrandingText "${PRODUCT_NAME} ${APP_VERSION}"
OutFile "${OUT_FILE}"

; user 范围不申请提权：装到用户目录，卸载也不需要管理员。
!if "${INSTALL_SCOPE}" == "user"
  RequestExecutionLevel user
!else
  RequestExecutionLevel admin
!endif

!define MUI_ICON   "${ICON_FILE}"
!define MUI_UNICON "${ICON_FILE}"
!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_NOAUTOCLOSE
; 不在这里「立即启动」：machine 范围的安装器是提权进程，从它启动的子进程也带管理员令牌，
; 程序会把配置写进管理员的用户目录，之后正常启动时看起来像「设置丢了」。
; 让用户从开始菜单启动（普通权限），这是 Windows 的常规做法。
; 最后一页勾「创建桌面快捷方式」：用一个函数代替「读我」，勾了才建（默认不勾）。
; 静默安装（/S）不会走这一页，所以脚本安装不带桌面图标——这是有意的。
!define MUI_FINISHPAGE_SHOWREADME ""
!define MUI_FINISHPAGE_SHOWREADME_NOTCHECKED
!define MUI_FINISHPAGE_SHOWREADME_TEXT "在桌面创建快捷方式"
!define MUI_FINISHPAGE_SHOWREADME_FUNCTION CreateDesktopShortcut

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

; 系统是中文就中文，否则英文（MUI 按用户语言自动选第一个命中的）
!insertmacro MUI_LANGUAGE "SimpChinese"
!insertmacro MUI_LANGUAGE "English"

; ---- 安装目录与注册表口径 ----

!if "${INSTALL_SCOPE}" == "user"
  InstallDir "$LOCALAPPDATA\Programs\${PRODUCT_NAME}"
  InstallDirRegKey HKCU "${UNINST_KEY}" "InstallLocation"
  ; 本产品的默认目录：装错目录时用来改回去（见 .onInit / .onVerifyInstDir）。
  !define DEFAULT_INSTDIR "$LOCALAPPDATA\Programs\${PRODUCT_NAME}"
!else
  InstallDir "$PROGRAMFILES64\${PRODUCT_NAME}"
  InstallDirRegKey HKLM "${UNINST_KEY}" "InstallLocation"
  !define DEFAULT_INSTDIR "$PROGRAMFILES64\${PRODUCT_NAME}"
!endif

ShowInstDetails show
ShowUninstDetails show

Var PrevLoc
Var Running
Var WarnedForeign

; 目录页校验：不许装进另一个产品的目录。
;
; 两个产品共用一个「api-doc-go」品牌，但安装目录 / 卸载键 / exe 名都不同
; （Wails 版是 api-doc-go\api-doc-go\api-doc-client.exe，本产品是 api-doc-go-viva\api-doc-viva.exe）。
; 真装进对方目录，会把对方的 uninstall.exe 覆盖成本产品的：之后「应用和功能」里
; 两条目的卸载程序指向同一个文件，互相删不干净，快捷方式也会打架。
; 调 Abort 会让「下一步」置灰，用户只能换目录。
Function .onVerifyInstDir
  ${If} ${FileExists} "$INSTDIR\api-doc-client.exe"
  ${OrIf} ${FileExists} "$INSTDIR\api-doc-go.exe"
    Abort
    ${If} $WarnedForeign != "1"
      StrCpy $WarnedForeign "1"
      MessageBox MB_OK|MB_ICONEXCLAMATION \
        "这个目录里有 api-doc-client.exe，是 api-doc-go（Wails 版）的安装目录。$\r$\n$\r$\n两个产品可以并存，但要各装各的目录：装在一起会覆盖对方的卸载程序。$\r$\n$\r$\n请换一个目录，默认是本产品自己的目录。"
    ${EndIf}
  ${EndIf}
FunctionEnd

; 检测客户端是否在跑。
; 用 tasklist 的 CSV 输出判断：有该进程时输出以引号开头（"xxx.exe","1234",...），
; 没有时是本地化的 INFO 文本。这样中英文系统都成立，比解析汉字可靠。
Function CheckRunning
  StrCpy $Running "0"
  nsExec::ExecToStack 'cmd /c tasklist /FI "IMAGENAME eq ${APP_EXE}" /FO CSV /NH'
  Pop $0
  Pop $1
  StrCpy $2 $1 1
  ${If} $2 == '"'
    StrCpy $Running "1"
  ${EndIf}
FunctionEnd

; 静默结束进程：nsExec 隐藏控制台窗口，输出进安装日志。
Function KillRunning
  ${If} $Running == "1"
    DetailPrint "结束正在运行的 ${APP_EXE}"
    nsExec::ExecToLog '"$SYSDIR\taskkill.exe" /IM "${APP_EXE}" /F'
    Pop $0
    Sleep 500
  ${EndIf}
FunctionEnd

Function .onInit
  SetRegView 64
  Call CheckRunning

  ; 沿用它上次装到哪：别因为用户改过目录就装出第二份。
  !if "${INSTALL_SCOPE}" == "user"
    ReadRegStr $PrevLoc HKCU "${UNINST_KEY}" "InstallLocation"
  !else
    ReadRegStr $PrevLoc HKLM "${UNINST_KEY}" "InstallLocation"
  !endif
  ${If} $PrevLoc != ""
    StrCpy $INSTDIR $PrevLoc
  ${EndIf}

  ; 上次记录的目录如果属于另一个产品，不能沿用：装进去会把对方覆盖掉。
  ; 交互安装有目录页兜底（见 .onVerifyInstDir）；静默安装没有页面，只能在这里中止。
  ${If} ${FileExists} "$INSTDIR\api-doc-client.exe"
  ${OrIf} ${FileExists} "$INSTDIR\api-doc-go.exe"
    ${If} $INSTDIR != "${DEFAULT_INSTDIR}"
      StrCpy $INSTDIR "${DEFAULT_INSTDIR}"
      ${If} ${Silent}
        DetailPrint "上次的安装目录属于 api-doc-go，静默安装改用 ${DEFAULT_INSTDIR}"
      ${Else}
        MessageBox MB_OK|MB_ICONINFORMATION \
          "上次记录的安装目录属于 api-doc-go（Wails 版）。$\r$\n$\r$\n两个产品可以并存，但要各装各的目录；已改回本产品的默认目录 ${DEFAULT_INSTDIR}。"
      ${EndIf}
    ${Else}
      ; 默认目录本身就是对方的地盘：静默安装没有目录页可以拦，直接失败退出。
      ${If} ${Silent}
        DetailPrint "${DEFAULT_INSTDIR} 属于 api-doc-go，静默安装中止（请用 /D= 指定另一个目录）"
        SetErrorLevel 3
        Quit
      ${EndIf}
    ${EndIf}
  ${EndIf}

  ; 客户端在跑就先处理：交互模式问一句，静默安装直接结束进程。
  ${If} $Running == "1"
    ${If} ${Silent}
      DetailPrint "检测到客户端正在运行，静默安装：直接结束进程"
      Call KillRunning
    ${Else}
      MessageBox MB_OKCANCEL|MB_ICONEXCLAMATION \
        "检测到 ${PRODUCT_NAME} 正在运行。$\r$\n$\r$\n安装会替换程序文件，未保存的修改会丢失。$\r$\n$\r$\n选「确定」由安装程序关闭它，选「取消」请自行退出后重试。" \
        IDOK app_running_kill
      Abort
      app_running_kill:
      Call KillRunning
    ${EndIf}
  ${EndIf}

  ; 有旧版就静默卸载：走「先卸再装」而不是覆盖，
  ; 免得旧版本多出来的文件、快捷方式留在新版本里。
  ${If} $PrevLoc != ""
  ${AndIf} ${FileExists} "$PrevLoc\uninstall.exe"
    DetailPrint "检测到旧版本（$PrevLoc），先静默卸载再安装"
    ; _?= 让卸载器原地跑（不复制到临时目录），必须是最后一个参数。
    nsExec::ExecToLog '"$PrevLoc\uninstall.exe" /S _?=$PrevLoc'
    Pop $0
    Sleep 800
  ${EndIf}
FunctionEnd

Function CreateDesktopShortcut
  CreateShortCut "$DESKTOP\${PRODUCT_NAME}.lnk" "$INSTDIR\${APP_EXE}" 0 "$INSTDIR\icon.ico" 0
FunctionEnd

Section "主程序（必需）" SEC_MAIN
  SectionIn RO
  SetOutPath $INSTDIR
  SetOverwrite on

  File "${STAGE_DIR}\${APP_EXE}"
  ; 快捷方式与「应用和功能」里的图标都用它：exe 本身没有嵌图标资源
  ; （交叉编译不带 windres），所以让它挨着程序放。
  File "/oname=$INSTDIR\icon.ico" "${ICON_FILE}"

  WriteUninstaller "$INSTDIR\uninstall.exe"

  ; 「应用和功能」里的条目
  !if "${INSTALL_SCOPE}" == "user"
    WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "${PRODUCT_NAME}"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${APP_VERSION}"
    WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "${COMPANY_NAME}"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\icon.ico"
    WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
    WriteRegStr HKCU "${UNINST_KEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
    WriteRegDWORD HKCU "${UNINST_KEY}" "NoModify" 1
    WriteRegDWORD HKCU "${UNINST_KEY}" "NoRepair" 1
  !else
    WriteRegStr HKLM "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
    WriteRegStr HKLM "${UNINST_KEY}" "DisplayName" "${PRODUCT_NAME}"
    WriteRegStr HKLM "${UNINST_KEY}" "DisplayVersion" "${APP_VERSION}"
    WriteRegStr HKLM "${UNINST_KEY}" "Publisher" "${COMPANY_NAME}"
    WriteRegStr HKLM "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\icon.ico"
    WriteRegStr HKLM "${UNINST_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
    WriteRegStr HKLM "${UNINST_KEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
    WriteRegDWORD HKLM "${UNINST_KEY}" "NoModify" 1
    WriteRegDWORD HKLM "${UNINST_KEY}" "NoRepair" 1
  !endif

  ; EstimatedSize 单位是 KB
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  !if "${INSTALL_SCOPE}" == "user"
    WriteRegDWORD HKCU "${UNINST_KEY}" "EstimatedSize" $0
  !else
    WriteRegDWORD HKLM "${UNINST_KEY}" "EstimatedSize" $0
  !endif

  CreateDirectory "$SMPROGRAMS\${PRODUCT_NAME}"
  CreateShortCut "$SMPROGRAMS\${PRODUCT_NAME}\${PRODUCT_NAME}.lnk" "$INSTDIR\${APP_EXE}" 0 "$INSTDIR\icon.ico" 0
  CreateShortCut "$SMPROGRAMS\${PRODUCT_NAME}\卸载 ${PRODUCT_NAME}.lnk" "$INSTDIR\uninstall.exe"
SectionEnd

Section "Uninstall"
  SetRegView 64

  ; 卸载前先关掉：否则 exe 被占用，删除会失败。
  Call un.CheckRunning
  Call un.KillRunning

  Delete "$INSTDIR\${APP_EXE}"
  Delete "$INSTDIR\icon.ico"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"

  Delete "$SMPROGRAMS\${PRODUCT_NAME}\${PRODUCT_NAME}.lnk"
  Delete "$SMPROGRAMS\${PRODUCT_NAME}\卸载 ${PRODUCT_NAME}.lnk"
  RMDir "$SMPROGRAMS\${PRODUCT_NAME}"
  Delete "$DESKTOP\${PRODUCT_NAME}.lnk"

  ; 只清自己的键；用户的配置目录（Cookie、偏好、密钥）保留——
  ; 卸载程序不该顺手删掉登录状态与密钥。
  !if "${INSTALL_SCOPE}" == "user"
    DeleteRegKey HKCU "${UNINST_KEY}"
  !else
    DeleteRegKey HKLM "${UNINST_KEY}"
  !endif
SectionEnd

; 卸载器自己也需要这两个函数（NSIS 的 un. 前缀是另一份函数）。
Function un.CheckRunning
  StrCpy $Running "0"
  nsExec::ExecToStack 'cmd /c tasklist /FI "IMAGENAME eq ${APP_EXE}" /FO CSV /NH'
  Pop $0
  Pop $1
  StrCpy $2 $1 1
  ${If} $2 == '"'
    StrCpy $Running "1"
  ${EndIf}
FunctionEnd

Function un.KillRunning
  ${If} $Running == "1"
    DetailPrint "结束正在运行的 ${APP_EXE}"
    nsExec::ExecToLog '"$SYSDIR\taskkill.exe" /IM "${APP_EXE}" /F'
    Pop $0
    Sleep 500
  ${EndIf}
FunctionEnd
