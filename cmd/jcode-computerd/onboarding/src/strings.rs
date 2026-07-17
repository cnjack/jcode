//! Onboarding copy in the five locales the product already ships
//! (web/src/i18n/locales). Picked from the *system* preferred language — this
//! window belongs to the OS permission ceremony, not to a jcode session, so it
//! follows the Mac's language the way the System Settings pane it points at
//! does.

pub struct Strings {
    pub title: &'static str,
    pub subtitle: &'static str,
    pub ax_title: &'static str,
    pub ax_desc: &'static str,
    pub sr_title: &'static str,
    pub sr_desc: &'static str,
    pub allow: &'static str,
    pub granted: &'static str,
    pub all_set: &'static str,
    pub drag_hint: &'static str,
    pub app_name: &'static str,
}

pub const EN: Strings = Strings {
    title: "Enable jcode Computer Use",
    subtitle: "jcode Computer Use needs these permissions to use apps on your Mac. \
               They are only used when you ask jcode to perform tasks.",
    ax_title: "Accessibility",
    ax_desc: "Lets jcode read app interfaces and click, type, and scroll for you",
    sr_title: "Screen Recording",
    sr_desc: "Lets jcode take window screenshots to see what's on screen",
    allow: "Allow",
    granted: "Allowed",
    all_set: "All set — jcode can now use apps on this Mac",
    drag_hint: "Drag jcode Computer Use into the list above to allow Accessibility",
    app_name: "jcode Computer Use",
};

pub const ZH_HANS: Strings = Strings {
    title: "启用 jcode Computer Use",
    subtitle: "jcode Computer Use 需要以下权限，才能在这台 Mac 上操作应用。这些权限只在你让 jcode 执行任务时使用。",
    ax_title: "辅助功能",
    ax_desc: "允许 jcode 读取应用界面，并代替你点击、输入和滚动",
    sr_title: "屏幕录制",
    sr_desc: "允许 jcode 截取窗口截图，以了解屏幕上的内容",
    allow: "允许",
    granted: "已允许",
    all_set: "已就绪 — jcode 现在可以操作这台 Mac 上的应用了",
    drag_hint: "将 jcode Computer Use 拖入上方列表，以允许辅助功能",
    app_name: "jcode Computer Use",
};

pub const ZH_HANT: Strings = Strings {
    title: "啟用 jcode Computer Use",
    subtitle: "jcode Computer Use 需要以下權限，才能在這部 Mac 上操作應用程式。這些權限只在你要求 jcode 執行任務時使用。",
    ax_title: "輔助使用",
    ax_desc: "允許 jcode 讀取應用程式介面，並代替你按一下、輸入和捲動",
    // Apple's zh_TW name for the Screen Recording pane.
    sr_title: "螢幕錄影",
    sr_desc: "允許 jcode 擷取視窗截圖，以了解螢幕上的內容",
    allow: "允許",
    granted: "已允許",
    all_set: "已就緒 — jcode 現在可以操作這部 Mac 上的應用程式了",
    drag_hint: "將 jcode Computer Use 拖移到上方列表，以允許輔助使用",
    app_name: "jcode Computer Use",
};

pub const JA: Strings = Strings {
    title: "jcode Computer Use を有効にする",
    subtitle: "jcode Computer Use がこの Mac のアプリを操作するには、以下の権限が必要です。これらの権限は jcode にタスクを依頼したときにのみ使用されます。",
    ax_title: "アクセシビリティ",
    ax_desc: "jcode がアプリの画面を読み取り、クリック・入力・スクロールを代行できるようにします",
    sr_title: "画面収録",
    sr_desc: "jcode がウインドウのスクリーンショットを撮り、画面の内容を把握できるようにします",
    allow: "許可",
    granted: "許可済み",
    all_set: "設定完了 — jcode がこの Mac のアプリを操作できるようになりました",
    drag_hint: "jcode Computer Use を上のリストにドラッグして、アクセシビリティを許可してください",
    app_name: "jcode Computer Use",
};

pub const KO: Strings = Strings {
    title: "jcode Computer Use 활성화",
    subtitle: "jcode Computer Use가 이 Mac의 앱을 제어하려면 다음 권한이 필요합니다. 이 권한은 jcode에 작업을 요청할 때만 사용됩니다.",
    ax_title: "손쉬운 사용",
    ax_desc: "jcode가 앱 인터페이스를 읽고 클릭·입력·스크롤을 대신할 수 있게 합니다",
    sr_title: "화면 기록",
    sr_desc: "jcode가 윈도우 스크린샷을 찍어 화면 내용을 파악할 수 있게 합니다",
    allow: "허용",
    granted: "허용됨",
    all_set: "설정 완료 — 이제 jcode가 이 Mac의 앱을 사용할 수 있습니다",
    drag_hint: "위 목록으로 jcode Computer Use를 드래그하여 손쉬운 사용을 허용하세요",
    app_name: "jcode Computer Use",
};

/// Match the product's locale fallback: exact zh-Hant spellings first, then
/// the zh prefix → zh-Hans, then ja/ko, else English.
pub fn pick(preferred: &[String]) -> &'static Strings {
    for lang in preferred {
        let l = lang.to_ascii_lowercase();
        if l.starts_with("zh-hant") || l.starts_with("zh-tw") || l.starts_with("zh-hk")
            || l.starts_with("zh-mo")
        {
            return &ZH_HANT;
        }
        if l.starts_with("zh") {
            return &ZH_HANS;
        }
        if l.starts_with("ja") {
            return &JA;
        }
        if l.starts_with("ko") {
            return &KO;
        }
        if l.starts_with("en") {
            return &EN;
        }
    }
    &EN
}
