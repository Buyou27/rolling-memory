<template>
    <div class="default-main rm-help-portal">
        <!-- ==================== 顶部说明：文档不再内置副本，只提供官方入口 ==================== -->
        <section class="portal-hero">
            <div class="hero-copy">
                <span class="hero-badge">
                    <Icon name="lucide-book-open-check" :size="13" />
                    <span>{{ t('help.badge') }}</span>
                </span>
                <h1 class="hero-title">{{ t('help.title') }}</h1>
                <p class="hero-subtitle">{{ t('help.subtitle') }}</p>
                <div class="hero-actions">
                    <button type="button" class="hero-primary" @click="openChooser">
                        <Icon name="lucide-mouse-pointer-click" :size="15" />
                        <span>{{ t('help.chooseAgain') }}</span>
                    </button>
                    <span class="hero-hint">
                        <Icon name="lucide-shield-check" :size="13" />
                        <span>{{ t('help.externalHint') }}</span>
                    </span>
                </div>
            </div>
            <div class="hero-decoration" aria-hidden="true">
                <Icon name="lucide-book-open-text" :size="148" />
            </div>
        </section>

        <!-- ==================== 双站入口卡片 ==================== -->
        <section class="portal-grid" :aria-label="t('help.chooseTitle')">
            <a
                v-for="site in sites"
                :key="site.key"
                class="portal-card"
                :class="`is-${site.key}`"
                :style="{ '--site-color': site.color }"
                :href="site.url"
                target="_blank"
                rel="noopener noreferrer"
                :aria-label="`${site.name} — ${site.desc} (${t('help.opensInNewTab')})`"
                @click="rememberHost(site.key)"
            >
                <span class="portal-icon">
                    <HelpBrandMark :name="site.key" />
                </span>
                <span class="portal-body">
                    <span class="portal-title-row">
                        <strong>{{ site.name }}</strong>
                        <em class="portal-tag">{{ site.tag }}</em>
                        <em v-if="lastHost === site.key" class="portal-last">
                            <Icon name="lucide-history" :size="11" />
                            <span>{{ t('help.lastVisited') }}</span>
                        </em>
                    </span>
                    <span class="portal-desc">{{ site.desc }}</span>
                    <code class="portal-path">{{ site.path }}</code>
                </span>
                <span class="portal-go" aria-hidden="true">
                    <Icon name="lucide-arrow-up-right" :size="17" />
                </span>
            </a>
        </section>

        <!-- ==================== 口径与细节 ==================== -->
        <section class="portal-foot">
            <div class="foot-note">
                <Icon name="lucide-info" :size="15" />
                <span>{{ t('help.mirrorNotice') }}</span>
            </div>
            <div class="foot-perks">
                <span class="perk"><Icon name="lucide-git-merge" :size="13" />{{ t('help.perks.sameRepo') }}</span>
                <span class="perk"><Icon name="lucide-refresh-cw" :size="13" />{{ t('help.perks.synced') }}</span>
                <span class="perk"><Icon name="lucide-external-link" :size="13" />{{ t('help.perks.newTab') }}</span>
            </div>
            <p class="foot-version">rocareer/rolling-memory · v{{ releaseVersion }} · {{ t('help.footerNote') }}</p>
        </section>

        <!-- ==================== 打开帮助中心即弹出：选择 GitHub / Gitee ==================== -->
        <el-dialog
            v-model="chooserOpen"
            class="rm-help-chooser"
            :title="t('help.chooseTitle')"
            width="600px"
            align-center
            append-to-body
            :show-close="false"
            :close-on-click-modal="true"
            :close-on-press-escape="true"
        >
            <template #header>
                <div class="chooser-head">
                    <span class="chooser-head-icon">
                        <Icon name="lucide-book-open" :size="19" />
                    </span>
                    <div class="chooser-head-copy">
                        <h2 class="chooser-title">{{ t('help.chooseTitle') }}</h2>
                        <p class="chooser-subtitle">{{ t('help.chooseSubtitle') }}</p>
                    </div>
                    <button type="button" class="chooser-close" :aria-label="t('help.close')" @click="chooserOpen = false">
                        <Icon name="lucide-x" :size="16" />
                    </button>
                </div>
            </template>

            <div class="chooser-options" @keydown="onChooserKeydown">
                <a
                    v-for="site in sites"
                    :key="site.key"
                    class="chooser-option"
                    :class="`is-${site.key}`"
                    :style="{ '--site-color': site.color }"
                    :href="site.url"
                    target="_blank"
                    rel="noopener noreferrer"
                    :aria-label="`${site.name} — ${site.desc} (${t('help.opensInNewTab')})`"
                    @click="chooseSite(site.key)"
                >
                    <span class="chooser-icon">
                        <HelpBrandMark :name="site.key" />
                    </span>
                    <span class="chooser-copy">
                        <span class="chooser-name-row">
                            <strong>{{ site.name }}</strong>
                            <em class="chooser-tag">{{ site.tag }}</em>
                            <em v-if="lastHost === site.key" class="chooser-last">{{ t('help.lastVisited') }}</em>
                        </span>
                        <span class="chooser-desc">{{ site.desc }}</span>
                    </span>
                    <span class="chooser-go" aria-hidden="true">
                        <Icon name="lucide-arrow-up-right" :size="16" />
                    </span>
                </a>
            </div>

            <template #footer>
                <div class="chooser-foot">
                    <span class="chooser-hint">
                        <kbd>Esc</kbd>
                        <span>{{ t('help.escHint') }}</span>
                    </span>
                    <button type="button" class="chooser-stay" @click="chooserOpen = false">
                        {{ t('help.stayHere') }}
                    </button>
                </div>
            </template>
        </el-dialog>
    </div>
</template>

<script setup lang="ts">
import { computed, onActivated, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import HelpBrandMark from './brandMark.vue'

// 构建期注入的发布版本（单一真源 = 仓根 VERSION，经 setup 常量桥接进模板）。
const releaseVersion = __RM_VERSION__

defineOptions({
    name: 'help/docs',
})

type SiteKey = 'github' | 'gitee'

interface DocSite {
    key: SiteKey
    name: string
    tag: string
    desc: string
    path: string
    url: string
    color: string
}

const DOCS_URLS: Record<SiteKey, string> = {
    github: 'https://github.com/rocareer/rolling-memory/tree/main/docs',
    gitee: 'https://gitee.com/rocareer/Rolling-Memory/tree/main/docs',
}

const LAST_HOST_KEY = 'rm-help-docs-host'

const { t } = useI18n()

const chooserOpen = ref(false)
const lastHost = ref<SiteKey | ''>('')

const sites = computed<DocSite[]>(() => [
    {
        key: 'github',
        name: 'GitHub',
        tag: t('help.githubTag'),
        desc: t('help.githubDesc'),
        path: 'rocareer/rolling-memory · docs/',
        url: DOCS_URLS.github,
        color: '#24292f',
    },
    {
        key: 'gitee',
        name: 'Gitee',
        tag: t('help.giteeTag'),
        desc: t('help.giteeDesc'),
        path: 'rocareer/Rolling-Memory · docs/',
        url: DOCS_URLS.gitee,
        color: '#c71d23',
    },
])

function readLastHost() {
    try {
        const saved = window.localStorage.getItem(LAST_HOST_KEY)
        if (saved === 'github' || saved === 'gitee') lastHost.value = saved
    } catch {
        // 隐私模式或存储被禁用时保持无记录状态，不影响入口使用。
    }
}

function rememberHost(key: SiteKey) {
    lastHost.value = key
    try {
        window.localStorage.setItem(LAST_HOST_KEY, key)
    } catch {
        // 忽略存储失败：本次会话内的选择仍然生效。
    }
}

function openChooser() {
    chooserOpen.value = true
}

function chooseSite(key: SiteKey) {
    rememberHost(key)
    chooserOpen.value = false
}

function chooserOptions(): HTMLAnchorElement[] {
    const dialogs = document.querySelectorAll<HTMLElement>('.rm-help-chooser')
    const current = dialogs[dialogs.length - 1]
    return current ? Array.from(current.querySelectorAll<HTMLAnchorElement>('.chooser-option')) : []
}

let focusTimer: number | null = null

function focusChooserOption() {
    // Element Plus 的焦点陷阱在弹窗展开后仍会把焦点收回到容器上；
    // 延后一帧再落到首个站点，键盘用户按下方向键即可切换。
    if (focusTimer !== null) window.clearTimeout(focusTimer)
    focusTimer = window.setTimeout(() => {
        focusTimer = null
        chooserOptions()[0]?.focus()
    }, 120)
}

watch(chooserOpen, (open) => {
    if (open) focusChooserOption()
})

function onChooserKeydown(event: KeyboardEvent) {
    const keys = ['ArrowDown', 'ArrowRight', 'ArrowUp', 'ArrowLeft']
    if (!keys.includes(event.key)) return
    const options = chooserOptions()
    if (!options.length) return
    event.preventDefault()
    const current = options.indexOf(document.activeElement as HTMLAnchorElement)
    const forward = event.key === 'ArrowDown' || event.key === 'ArrowRight'
    const next = current < 0 ? 0 : (current + (forward ? 1 : options.length - 1)) % options.length
    options[next]?.focus()
}

function activateChooser() {
    readLastHost()
    chooserOpen.value = true
}

onMounted(activateChooser)
// 后台标签页启用了 keep-alive：每次点开帮助中心都重新弹出站点选择。
onActivated(activateChooser)
onBeforeUnmount(() => {
    if (focusTimer !== null) window.clearTimeout(focusTimer)
})
</script>

<style scoped lang="scss">
.rm-help-portal {
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding-bottom: 24px;
}

/* ==================== 顶部说明 ==================== */
.portal-hero {
    position: relative;
    overflow: hidden;
    padding: 26px 30px;
    border-radius: 16px;
    background:
        radial-gradient(560px circle at 88% 8%, rgba(14, 165, 233, 0.16), transparent 62%),
        radial-gradient(460px circle at 4% 96%, rgba(37, 99, 235, 0.12), transparent 58%), var(--el-bg-color-overlay);
    border: 1px solid var(--el-border-color-lighter);
    box-shadow: 0 4px 22px rgba(15, 23, 42, 0.05);
}

.hero-copy {
    position: relative;
    z-index: 1;
    max-width: 760px;
}

.hero-badge {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 4px 11px;
    border-radius: 999px;
    font-size: 12px;
    font-weight: 600;
    color: var(--el-color-primary);
    background: var(--el-color-primary-light-9);
    border: 1px solid var(--el-color-primary-light-7);
}

.hero-title {
    margin: 12px 0 8px;
    font-size: 26px;
    line-height: 1.2;
    font-weight: 700;
    letter-spacing: -0.02em;
    color: var(--el-text-color-primary);
}

.hero-subtitle {
    margin: 0;
    max-width: 660px;
    font-size: 13.5px;
    line-height: 1.7;
    color: var(--el-text-color-secondary);
}

.hero-actions {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
    margin-top: 18px;
}

.hero-primary {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    height: 36px;
    padding: 0 16px;
    border: 1px solid transparent;
    border-radius: 9px;
    font-size: 13px;
    font-weight: 600;
    color: #ffffff;
    background: linear-gradient(135deg, #2563eb, #0284c7);
    box-shadow: 0 6px 16px rgba(37, 99, 235, 0.24);
    cursor: pointer;
    transition:
        transform 0.16s ease,
        box-shadow 0.16s ease,
        opacity 0.16s ease;

    &:hover {
        transform: translateY(-1px);
        box-shadow: 0 8px 20px rgba(37, 99, 235, 0.3);
    }

    &:active {
        transform: translateY(0);
        opacity: 0.92;
    }

    &:focus-visible {
        outline: 2px solid var(--el-color-primary-light-5);
        outline-offset: 2px;
    }
}

.hero-hint {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: 12px;
    color: var(--el-text-color-secondary);
}

.hero-decoration {
    position: absolute;
    right: 26px;
    bottom: -24px;
    color: var(--el-color-primary);
    opacity: 0.08;
    pointer-events: none;
}

/* ==================== 双站入口卡片 ==================== */
.portal-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 14px;
}

.portal-card {
    position: relative;
    display: flex;
    align-items: flex-start;
    gap: 16px;
    padding: 20px 22px;
    border-radius: 14px;
    border: 1px solid var(--el-border-color-lighter);
    background: var(--el-bg-color-overlay);
    box-shadow: 0 2px 10px rgba(15, 23, 42, 0.03);
    text-decoration: none;
    color: inherit;
    transition:
        transform 0.18s ease,
        border-color 0.18s ease,
        box-shadow 0.18s ease;

    &::before {
        content: '';
        position: absolute;
        inset: 0 auto 0 0;
        width: 3px;
        border-radius: 14px 0 0 14px;
        background: var(--site-color);
        opacity: 0;
        transition: opacity 0.18s ease;
    }

    &:hover {
        transform: translateY(-2px);
        border-color: var(--site-color);
        box-shadow: 0 10px 26px rgba(15, 23, 42, 0.1);

        &::before {
            opacity: 1;
        }

        .portal-go {
            transform: translate(2px, -2px);
            color: var(--site-color);
        }
    }

    &:focus-visible {
        outline: 2px solid var(--site-color);
        outline-offset: 2px;
    }
}

.portal-icon {
    flex: none;
    display: grid;
    place-items: center;
    width: 48px;
    height: 48px;
    padding: 12px;
    border-radius: 13px;
    color: #ffffff;
    background: var(--site-color);
    box-shadow:
        0 6px 16px rgba(15, 23, 42, 0.16),
        inset 0 0 0 1px rgba(255, 255, 255, 0.08);
}

.portal-body {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
    flex: 1;
}

.portal-title-row {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;

    strong {
        font-size: 16px;
        font-weight: 650;
        color: var(--el-text-color-primary);
    }
}

.portal-tag {
    padding: 1px 7px;
    border-radius: 999px;
    font-style: normal;
    font-size: 11px;
    font-weight: 600;
    color: var(--el-text-color-primary);
    background: color-mix(in srgb, var(--site-color) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--site-color) 28%, transparent);
}

.portal-last {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    font-style: normal;
    font-size: 11px;
    color: var(--el-text-color-secondary);
}

.portal-desc {
    font-size: 12.5px;
    line-height: 1.55;
    color: var(--el-text-color-regular);
}

.portal-path {
    font-family: var(--el-font-family-monospace, monospace);
    font-size: 11.5px;
    color: var(--el-text-color-secondary);
    word-break: break-all;
}

.portal-go {
    flex: none;
    align-self: center;
    color: var(--el-text-color-secondary);
    transition:
        transform 0.18s ease,
        color 0.18s ease;
}

/* ==================== 口径说明 ==================== */
.portal-foot {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 16px 20px;
    border-radius: 12px;
    background: var(--el-fill-color-lighter);
    border: 1px dashed var(--el-border-color);
}

.foot-note {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    font-size: 12.5px;
    line-height: 1.65;
    color: var(--el-text-color-regular);

    .icon {
        flex: none;
        margin-top: 2px;
        color: var(--el-color-primary);
    }
}

.foot-perks {
    display: flex;
    align-items: center;
    gap: 14px;
    flex-wrap: wrap;
}

.perk {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: 11.5px;
    color: var(--el-text-color-secondary);
}

.foot-version {
    margin: 0;
    font-family: var(--el-font-family-monospace, monospace);
    font-size: 11px;
    color: var(--el-text-color-placeholder);
}

@media (max-width: 760px) {
    .portal-grid {
        grid-template-columns: 1fr;
    }

    .portal-hero {
        padding: 22px 20px;
    }

    .hero-decoration {
        display: none;
    }
}

@media (prefers-reduced-motion: reduce) {
    .portal-card,
    .portal-go,
    .hero-primary {
        transition: none;
    }
}
</style>

<style lang="scss">
/* 选择弹窗挂载在 body 上，样式不能限定在页面根节点内。 */
.rm-help-chooser {
    border-radius: 16px;
    overflow: hidden;
    border: 1px solid var(--el-border-color-lighter);
    box-shadow: 0 24px 64px rgba(15, 23, 42, 0.22);

    .el-dialog__header {
        margin: 0;
        padding: 20px 22px 0;
    }

    .el-dialog__body {
        padding: 16px 22px 8px;
    }

    .el-dialog__footer {
        padding: 4px 22px 18px;
    }
}

.rm-help-chooser .chooser-head {
    display: flex;
    align-items: flex-start;
    gap: 12px;
}

.rm-help-chooser .chooser-head-icon {
    flex: none;
    display: grid;
    place-items: center;
    width: 38px;
    height: 38px;
    border-radius: 11px;
    color: var(--el-color-primary);
    background: var(--el-color-primary-light-9);
    border: 1px solid var(--el-color-primary-light-8);
}

.rm-help-chooser .chooser-head-copy {
    flex: 1;
    min-width: 0;
}

.rm-help-chooser .chooser-title {
    margin: 0;
    font-size: 17px;
    font-weight: 650;
    color: var(--el-text-color-primary);
}

.rm-help-chooser .chooser-subtitle {
    margin: 4px 0 0;
    font-size: 12.5px;
    line-height: 1.6;
    color: var(--el-text-color-secondary);
}

.rm-help-chooser .chooser-close {
    flex: none;
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    margin-top: 1px;
    border: none;
    border-radius: 8px;
    color: var(--el-text-color-secondary);
    background: transparent;
    cursor: pointer;
    transition:
        color 0.15s ease,
        background 0.15s ease;

    &:hover {
        color: var(--el-text-color-primary);
        background: var(--el-fill-color);
    }

    &:focus-visible {
        outline: 2px solid var(--el-color-primary);
        outline-offset: 1px;
    }
}

.rm-help-chooser .chooser-options {
    display: flex;
    flex-direction: column;
    gap: 10px;
}

.rm-help-chooser .chooser-option {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 13px 15px;
    border-radius: 12px;
    border: 1px solid var(--el-border-color);
    background: var(--el-bg-color-overlay);
    color: inherit;
    text-decoration: none;
    transition:
        border-color 0.16s ease,
        background 0.16s ease,
        transform 0.16s ease;

    &:hover {
        transform: translateX(2px);
        border-color: var(--site-color);
        background: color-mix(in srgb, var(--site-color) 5%, transparent);
    }

    &:focus-visible {
        outline: 2px solid var(--site-color);
        outline-offset: 2px;
    }
}

.rm-help-chooser .chooser-icon {
    flex: none;
    display: grid;
    place-items: center;
    width: 40px;
    height: 40px;
    padding: 10px;
    border-radius: 11px;
    color: #ffffff;
    background: var(--site-color);
}

.rm-help-chooser .chooser-copy {
    display: flex;
    flex-direction: column;
    gap: 3px;
    flex: 1;
    min-width: 0;
}

.rm-help-chooser .chooser-name-row {
    display: flex;
    align-items: center;
    gap: 7px;
    flex-wrap: wrap;

    strong {
        font-size: 14px;
        font-weight: 650;
        color: var(--el-text-color-primary);
    }
}

.rm-help-chooser .chooser-tag {
    padding: 1px 7px;
    border-radius: 999px;
    font-style: normal;
    font-size: 10.5px;
    font-weight: 600;
    color: var(--el-text-color-primary);
    background: color-mix(in srgb, var(--site-color) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--site-color) 28%, transparent);
}

.rm-help-chooser .chooser-last {
    font-style: normal;
    font-size: 10.5px;
    color: var(--el-text-color-secondary);
}

.rm-help-chooser .chooser-desc {
    font-size: 12px;
    line-height: 1.5;
    color: var(--el-text-color-regular);
}

.rm-help-chooser .chooser-go {
    flex: none;
    color: var(--el-text-color-secondary);
}

.rm-help-chooser .chooser-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
}

.rm-help-chooser .chooser-hint {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 11.5px;
    color: var(--el-text-color-secondary);

    kbd {
        padding: 1px 6px;
        border-radius: 5px;
        border: 1px solid var(--el-border-color);
        background: var(--el-fill-color-light);
        font-family: var(--el-font-family-monospace, monospace);
        font-size: 10.5px;
    }
}

.rm-help-chooser .chooser-stay {
    border: none;
    background: transparent;
    padding: 4px 2px;
    font-size: 12px;
    color: var(--el-color-primary);
    cursor: pointer;

    &:hover {
        text-decoration: underline;
    }

    &:focus-visible {
        outline: 2px solid var(--el-color-primary);
        outline-offset: 2px;
        border-radius: 4px;
    }
}

@media (max-width: 640px) {
    .rm-help-chooser {
        width: calc(100vw - 28px) !important;
    }
}
</style>
