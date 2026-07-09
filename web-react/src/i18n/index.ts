import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import enBase from './locales/en'
import zhHansBase from './locales/zh-Hans'
import zhHantBase from './locales/zh-Hant'
import jaBase from './locales/ja'
import koBase from './locales/ko'

export const SUPPORTED_LOCALES = ['en', 'zh-Hans', 'zh-Hant', 'ja', 'ko'] as const
export type SupportedLocale = (typeof SUPPORTED_LOCALES)[number]

export const LOCALE_LABELS: Record<SupportedLocale, string> = {
  en: 'English',
  'zh-Hans': '简体中文',
  'zh-Hant': '繁體中文',
  ja: '日本語',
  ko: '한국어',
}

const STORAGE_KEY = 'jcode_locale'
const LEGACY_KEY = 'jcode-lang'
const FALLBACK: SupportedLocale = 'en'

const HTML_LANG: Record<SupportedLocale, string> = {
  en: 'en',
  'zh-Hans': 'zh-Hans',
  'zh-Hant': 'zh-Hant',
  ja: 'ja',
  ko: 'ko',
}

function isSupported(value: string | null | undefined): value is SupportedLocale {
  return !!value && (SUPPORTED_LOCALES as readonly string[]).includes(value)
}

function normalizeLegacy(value: string | null): SupportedLocale | null {
  if (!value) return null
  if (isSupported(value)) return value
  if (value === 'zh') return 'zh-Hans'
  return null
}

function browserLocale(): SupportedLocale {
  if (typeof navigator === 'undefined') return FALLBACK
  const tags = navigator.languages?.length ? navigator.languages : [navigator.language]
  for (const tag of tags) {
    const lower = tag.toLowerCase()
    if (lower === 'zh' || lower.startsWith('zh-cn') || lower.startsWith('zh-sg') || lower.startsWith('zh-hans')) return 'zh-Hans'
    if (lower.startsWith('zh-tw') || lower.startsWith('zh-hk') || lower.startsWith('zh-mo') || lower.startsWith('zh-hant')) return 'zh-Hant'
    const primary = lower.split('-')[0]
    if (primary === 'ja') return 'ja'
    if (primary === 'ko') return 'ko'
    if (primary === 'en') return 'en'
  }
  return FALLBACK
}

function initialLocale(): SupportedLocale {
  if (typeof localStorage === 'undefined') return FALLBACK
  return (
    normalizeLegacy(localStorage.getItem(STORAGE_KEY)) ||
    normalizeLegacy(localStorage.getItem(LEGACY_KEY)) ||
    browserLocale()
  )
}

function applyDocumentLang(locale: SupportedLocale) {
  if (typeof document !== 'undefined') {
    document.documentElement.lang = HTML_LANG[locale] ?? locale
  }
}

function isPlainRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

function deepMerge<T extends Record<string, unknown>>(base: T, override: Record<string, unknown>): T {
  const out: Record<string, unknown> = { ...base }
  for (const [key, value] of Object.entries(override)) {
    const prev = out[key]
    out[key] = isPlainRecord(prev) && isPlainRecord(value) ? deepMerge(prev, value) : value
  }
  return out as T
}

const resources = {
  en: {
    translation: {
      common: {
        enable: 'Enable',
        disable: 'Disable',
        reset: 'Reset',
        loading: 'Loading...',
      },
      nav: {
        newTask: 'New chat',
        chat: 'Chat',
        automations: 'Automations',
        channels: 'Channels',
        workspace: 'Workspace',
        settingsWithShortcut: 'Settings (⌘,)',
      },
      sidebar: {
        noConversations: 'No conversations yet',
        noTasks: 'No tasks',
        running: 'running',
        remoteReconnectRequired: 'Reconnect to that remote workspace before opening this conversation.',
        switchProjectFailed: 'Could not switch workspace for this conversation.',
        actions: {
          taskActions: 'Actions',
          pin: 'Pin',
          unpin: 'Unpin',
          archive: 'Archive',
          unarchive: 'Unarchive',
          markRead: 'Mark read',
          markUnread: 'Mark unread',
          delete: 'Delete',
        },
        filter: {
          title: 'Filter conversations',
          status: 'Status',
          statusAll: 'All',
          statusActive: 'Active',
          statusArchived: 'Archived',
          project: 'Project',
          projectAll: 'All projects',
          lastActivity: 'Last activity',
          activityAll: 'Any time',
          activityToday: 'Today',
          activityWeek: 'This week',
          activityMonth: 'This month',
          groupBy: 'Group by',
          groupProject: 'Project',
          groupDate: 'Date',
          sortBy: 'Sort by',
          sortRecency: 'Recency',
          sortName: 'Name',
          sortCreated: 'Created',
          time: 'Time',
          timeAll: 'All time',
          timeToday: 'Today',
          timeWeek: 'This week',
          timeMonth: 'This month',
          sort: 'Sort',
          sortRecent: 'Recent',
          sortTitle: 'Title',
        },
        dateBucket: {
          today: 'Today',
          yesterday: 'Yesterday',
          week: 'This Week',
          month: 'This Month',
          older: 'Older',
        },
        relativeTime: {
          now: 'now',
          minutes: '{{n}}m',
          hours: '{{n}}h',
          days: '{{n}}d',
        },
      },
      topbar: {
        plan: 'Plan',
        files: 'Files',
        changes: 'Changes',
        terminal: 'Terminal',
        status: {
          running: 'Running',
          connected: 'Connected',
          disconnected: 'Disconnected',
        },
        panelsHint: 'Panels · {{status}}  (⇧⌘P plan · ⇧⌘E files · ⇧⌘G changes · ⌘` terminal)',
        panelsMenu: 'Panels menu · {{status}}',
      },
      settings: {
        backToWorkspace: 'Close',
        tabs: {
          general: 'General',
          appearance: 'Appearance',
          providers: 'Providers',
          mcp: 'MCP',
          skills: 'Skills',
          browser: 'Browser',
          ssh: 'SSH',
          channels: 'Channels',
          shortcuts: 'Shortcuts',
          usage: 'Usage',
        },
        general: {
          title: 'General',
          serverState: 'Server state',
          serverOnline: 'Online',
          serverOffline: 'Offline',
          tokenUsage: 'Token usage',
          preferences: 'Preferences',
          defaultMode: 'Default mode',
          defaultModeDesc: 'Controls how much autonomy the agent has on each new chat.',
          autoApproveTitle: 'Auto-approve',
          autoApproveDesc: 'Automatically approve tool calls without prompting.',
          bleTitle: 'Bluetooth notifications',
          bleDesc: 'Use the desktop BLE status channel for nearby notifications.',
          languageTitle: 'Language',
          languageDesc: 'Interface language preference.',
          maxIterations: 'Max iterations',
          maxIterationsReadOnly: 'Configured in ~/.jcode/config.json and applied when a run starts.',
        },
      },
      chat: {
        modes: {
          approval: 'Ask for approval',
          plan: 'Plan',
          fullAccess: 'Full access',
        },
      },
    },
  },
  'zh-Hans': {
    translation: {
      common: { enable: '启用', disable: '停用', reset: '重置', loading: '加载中...' },
      nav: {
        newTask: '新聊天',
        chat: '聊天',
        automations: '自动化',
        channels: '渠道',
        workspace: '工作区',
        settingsWithShortcut: '设置 (⌘,)',
      },
      sidebar: {
        noConversations: '还没有会话',
        noTasks: '暂无任务',
        running: '运行中',
        remoteReconnectRequired: '请先重新连接该远程工作区，再打开这个会话。',
        switchProjectFailed: '无法切换到这个会话所在的工作区。',
        actions: {
          taskActions: '操作',
          pin: '置顶',
          unpin: '取消置顶',
          archive: '归档',
          unarchive: '取消归档',
          markRead: '标为已读',
          markUnread: '标为未读',
          delete: '删除',
        },
        filter: {
          title: '筛选会话',
          status: '状态',
          statusAll: '全部',
          statusActive: '活跃',
          statusArchived: '已归档',
          project: '项目',
          projectAll: '所有项目',
          lastActivity: '最近活动',
          activityAll: '任意时间',
          activityToday: '今天',
          activityWeek: '本周',
          activityMonth: '本月',
          groupBy: '分组方式',
          groupProject: '项目',
          groupDate: '日期',
          sortBy: '排序方式',
          sortRecency: '最近',
          sortName: '名称',
          sortCreated: '创建时间',
          time: '时间',
          timeAll: '全部时间',
          timeToday: '今天',
          timeWeek: '本周',
          timeMonth: '本月',
          sort: '排序',
          sortRecent: '最近',
          sortTitle: '标题',
        },
        dateBucket: { today: '今天', yesterday: '昨天', week: '本周', month: '本月', older: '更早' },
        relativeTime: { now: '刚刚', minutes: '{{n}}分钟', hours: '{{n}}小时', days: '{{n}}天' },
      },
      topbar: {
        plan: '计划',
        files: '文件',
        changes: '变更',
        terminal: '终端',
        status: { running: '运行中', connected: '已连接', disconnected: '未连接' },
        panelsHint: '面板 · {{status}}  (⇧⌘P 计划 · ⇧⌘E 文件 · ⇧⌘G 变更 · ⌘` 终端)',
        panelsMenu: '面板菜单 · {{status}}',
      },
      settings: {
        backToWorkspace: '关闭',
        tabs: {
          general: '通用',
          appearance: '外观',
          providers: '服务商',
          mcp: 'MCP',
          skills: '技能',
          browser: '浏览器',
          ssh: 'SSH',
          channels: '渠道',
          shortcuts: '快捷键',
          usage: '用量',
        },
        general: {
          title: '通用',
          serverState: '服务状态',
          serverOnline: '在线',
          serverOffline: '离线',
          tokenUsage: 'Token 用量',
          preferences: '偏好',
          defaultMode: '默认模式',
          defaultModeDesc: '控制每个新聊天中 agent 的自主程度。',
          autoApproveTitle: '自动批准',
          autoApproveDesc: '无需提示即可自动批准工具调用。',
          bleTitle: '蓝牙通知',
          bleDesc: '使用桌面 BLE 状态通道发送附近通知。',
          languageTitle: '语言',
          languageDesc: '界面语言偏好。',
          maxIterations: '最大迭代次数',
          maxIterationsReadOnly: '在 ~/.jcode/config.json 中配置，并在运行开始时生效。',
        },
      },
      chat: {
        modes: {
          approval: '请求批准',
          plan: '计划',
          fullAccess: '完全访问',
        },
      },
    },
  },
  'zh-Hant': {
    translation: {
      common: { enable: '啟用', disable: '停用', reset: '重置', loading: '載入中...' },
      nav: { newTask: '新聊天', chat: '聊天', automations: '自動化', channels: '渠道', workspace: '工作區', settingsWithShortcut: '設定 (⌘,)' },
      sidebar: {
        noConversations: '還沒有會話',
        noTasks: '暫無任務',
        running: '執行中',
        remoteReconnectRequired: '請先重新連線該遠端工作區，再開啟這個會話。',
        switchProjectFailed: '無法切換到這個會話所在的工作區。',
        actions: { taskActions: '操作', pin: '置頂', unpin: '取消置頂', archive: '封存', unarchive: '取消封存', markRead: '標為已讀', markUnread: '標為未讀', delete: '刪除' },
        filter: { title: '篩選會話', status: '狀態', statusAll: '全部', statusActive: '活躍', statusArchived: '已封存', project: '專案', projectAll: '所有專案', lastActivity: '最近活動', activityAll: '任意時間', activityToday: '今天', activityWeek: '本週', activityMonth: '本月', groupBy: '分組方式', groupProject: '專案', groupDate: '日期', sortBy: '排序方式', sortRecency: '最近', sortName: '名稱', sortCreated: '建立時間', time: '時間', timeAll: '全部時間', timeToday: '今天', timeWeek: '本週', timeMonth: '本月', sort: '排序', sortRecent: '最近', sortTitle: '標題' },
        dateBucket: { today: '今天', yesterday: '昨天', week: '本週', month: '本月', older: '更早' },
        relativeTime: { now: '剛剛', minutes: '{{n}}分鐘', hours: '{{n}}小時', days: '{{n}}天' },
      },
      topbar: {
        plan: '計劃', files: '檔案', changes: '變更', terminal: '終端機',
        status: { running: '執行中', connected: '已連線', disconnected: '未連線' },
        panelsHint: '面板 · {{status}}', panelsMenu: '面板選單 · {{status}}',
      },
      settings: {
        backToWorkspace: '關閉',
        tabs: { general: '一般', appearance: '外觀', providers: '服務商', mcp: 'MCP', skills: '技能', browser: '瀏覽器', ssh: 'SSH', channels: '渠道', shortcuts: '快捷鍵', usage: '用量' },
        general: { title: '一般', serverState: '服務狀態', serverOnline: '在線', serverOffline: '離線', tokenUsage: 'Token 用量', preferences: '偏好', defaultMode: '預設模式', defaultModeDesc: '控制每個新聊天中 agent 的自主程度。', autoApproveTitle: '自動批准', autoApproveDesc: '無需提示即可自動批准工具呼叫。', bleTitle: '藍牙通知', bleDesc: '使用桌面 BLE 狀態通道傳送附近通知。', languageTitle: '語言', languageDesc: '介面語言偏好。', maxIterations: '最大迭代次數', maxIterationsReadOnly: '在 ~/.jcode/config.json 中設定，並在執行開始時生效。' },
      },
      chat: { modes: { approval: '請求批准', plan: '計劃', fullAccess: '完全存取' } },
    },
  },
  ja: {
    translation: {
      common: { enable: '有効', disable: '無効', reset: 'リセット', loading: '読み込み中...' },
      nav: { newTask: '新規チャット', chat: 'チャット', automations: '自動化', channels: 'チャンネル', workspace: 'ワークスペース', settingsWithShortcut: '設定 (⌘,)' },
      sidebar: { noConversations: '会話はまだありません', noTasks: 'タスクはありません', running: '実行中', remoteReconnectRequired: 'この会話を開く前にリモートワークスペースへ再接続してください。', switchProjectFailed: 'この会話のワークスペースへ切り替えられませんでした。', actions: { taskActions: '操作', pin: 'ピン留め', unpin: 'ピン解除', archive: 'アーカイブ', unarchive: '復元', markRead: '既読にする', markUnread: '未読にする', delete: '削除' }, filter: { title: '会話をフィルター', status: '状態', statusAll: 'すべて', statusActive: 'アクティブ', statusArchived: 'アーカイブ済み', project: 'プロジェクト', projectAll: 'すべてのプロジェクト', lastActivity: '最終アクティビティ', activityAll: 'すべての期間', activityToday: '今日', activityWeek: '今週', activityMonth: '今月', groupBy: 'グループ化', groupProject: 'プロジェクト', groupDate: '日付', sortBy: '並び替え', sortRecency: '最近', sortName: '名前', sortCreated: '作成日', time: '時間', timeAll: '全期間', timeToday: '今日', timeWeek: '今週', timeMonth: '今月', sort: '並び替え', sortRecent: '最近', sortTitle: 'タイトル' }, dateBucket: { today: '今日', yesterday: '昨日', week: '今週', month: '今月', older: '以前' }, relativeTime: { now: '今', minutes: '{{n}}分', hours: '{{n}}時間', days: '{{n}}日' } },
      topbar: { plan: '計画', files: 'ファイル', changes: '変更', terminal: 'ターミナル', status: { running: '実行中', connected: '接続済み', disconnected: '未接続' }, panelsHint: 'パネル · {{status}}', panelsMenu: 'パネルメニュー · {{status}}' },
      settings: { backToWorkspace: '閉じる', tabs: { general: '一般', appearance: '外観', providers: 'プロバイダー', mcp: 'MCP', skills: 'スキル', browser: 'ブラウザー', ssh: 'SSH', channels: 'チャンネル', shortcuts: 'ショートカット', usage: '使用量' }, general: { title: '一般', serverState: 'サーバー状態', serverOnline: 'オンライン', serverOffline: 'オフライン', tokenUsage: 'トークン使用量', preferences: '設定', defaultMode: '既定モード', defaultModeDesc: '新しいチャットでの agent の自律性を制御します。', autoApproveTitle: '自動承認', autoApproveDesc: '確認なしでツール呼び出しを承認します。', bleTitle: 'Bluetooth 通知', bleDesc: 'デスクトップ BLE 状態チャンネルを使います。', languageTitle: '言語', languageDesc: 'UI 言語の設定。', maxIterations: '最大反復回数', maxIterationsReadOnly: '~/.jcode/config.json で設定され、実行開始時に適用されます。' } },
      chat: { modes: { approval: '承認を求める', plan: '計画', fullAccess: 'フルアクセス' } },
    },
  },
  ko: {
    translation: {
      common: { enable: '사용', disable: '사용 안 함', reset: '초기화', loading: '불러오는 중...' },
      nav: { newTask: '새 채팅', chat: '채팅', automations: '자동화', channels: '채널', workspace: '작업공간', settingsWithShortcut: '설정 (⌘,)' },
      sidebar: { noConversations: '아직 대화가 없습니다', noTasks: '작업이 없습니다', running: '실행 중', remoteReconnectRequired: '이 대화를 열기 전에 해당 원격 작업공간에 다시 연결하세요.', switchProjectFailed: '이 대화의 작업공간으로 전환하지 못했습니다.', actions: { taskActions: '작업', pin: '고정', unpin: '고정 해제', archive: '보관', unarchive: '보관 해제', markRead: '읽음으로 표시', markUnread: '읽지 않음으로 표시', delete: '삭제' }, filter: { title: '대화 필터', status: '상태', statusAll: '전체', statusActive: '활성', statusArchived: '보관됨', project: '프로젝트', projectAll: '모든 프로젝트', lastActivity: '최근 활동', activityAll: '전체 기간', activityToday: '오늘', activityWeek: '이번 주', activityMonth: '이번 달', groupBy: '그룹화', groupProject: '프로젝트', groupDate: '날짜', sortBy: '정렬', sortRecency: '최근', sortName: '이름', sortCreated: '생성일', time: '시간', timeAll: '전체 기간', timeToday: '오늘', timeWeek: '이번 주', timeMonth: '이번 달', sort: '정렬', sortRecent: '최근', sortTitle: '제목' }, dateBucket: { today: '오늘', yesterday: '어제', week: '이번 주', month: '이번 달', older: '이전' }, relativeTime: { now: '방금', minutes: '{{n}}분', hours: '{{n}}시간', days: '{{n}}일' } },
      topbar: { plan: '계획', files: '파일', changes: '변경', terminal: '터미널', status: { running: '실행 중', connected: '연결됨', disconnected: '연결 안 됨' }, panelsHint: '패널 · {{status}}', panelsMenu: '패널 메뉴 · {{status}}' },
      settings: { backToWorkspace: '닫기', tabs: { general: '일반', appearance: '모양', providers: '공급자', mcp: 'MCP', skills: '스킬', browser: '브라우저', ssh: 'SSH', channels: '채널', shortcuts: '단축키', usage: '사용량' }, general: { title: '일반', serverState: '서버 상태', serverOnline: '온라인', serverOffline: '오프라인', tokenUsage: '토큰 사용량', preferences: '환경설정', defaultMode: '기본 모드', defaultModeDesc: '새 채팅에서 agent 자율성을 제어합니다.', autoApproveTitle: '자동 승인', autoApproveDesc: '확인 없이 도구 호출을 승인합니다.', bleTitle: 'Bluetooth 알림', bleDesc: '데스크톱 BLE 상태 채널을 사용합니다.', languageTitle: '언어', languageDesc: '인터페이스 언어 설정.', maxIterations: '최대 반복', maxIterationsReadOnly: '~/.jcode/config.json 에서 설정되며 실행 시작 시 적용됩니다.' } },
      chat: { modes: { approval: '승인 요청', plan: '계획', fullAccess: '전체 접근' } },
    },
  },
} as const

const fullResources = {
  en: { translation: deepMerge(enBase, resources.en.translation) },
  'zh-Hans': { translation: deepMerge(zhHansBase, resources['zh-Hans'].translation) },
  'zh-Hant': { translation: deepMerge(zhHantBase, resources['zh-Hant'].translation) },
  ja: { translation: deepMerge(jaBase, resources.ja.translation) },
  ko: { translation: deepMerge(koBase, resources.ko.translation) },
}

void i18n.use(initReactI18next).init({
  resources: fullResources,
  lng: initialLocale(),
  fallbackLng: FALLBACK,
  interpolation: { escapeValue: false },
})

applyDocumentLang(i18n.language as SupportedLocale)

export async function setLocale(locale: SupportedLocale): Promise<void> {
  if (!isSupported(locale)) return
  await i18n.changeLanguage(locale)
  applyDocumentLang(locale)
  localStorage.setItem(STORAGE_KEY, locale)
  localStorage.removeItem(LEGACY_KEY)
}

export { i18n }
