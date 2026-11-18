<!-- plugin: rolling-memory system management -->
<template>
    <div class="default-main plugin-rolling-memory">
        <div class="sys-hero">
            <div class="sys-hero-titles">
                <span class="sys-hero-title">{{ t('plugin.rolling-memory.system.title') }}</span>
                <span class="sys-hero-sub">{{ t('plugin.rolling-memory.system.sub') }}</span>
            </div>
            <div class="sys-hero-actions">
                <el-button size="small" :loading="daemonBusy === 'status'" data-testid="btn-system-refresh" @click="loadDaemon">
                    {{ t('plugin.rolling-memory.system.refreshBtn') }}
                </el-button>
                <el-button size="small" plain data-testid="btn-system-update" @click="showSystemUpdateDialog = true">
                    {{ t('plugin.rolling-memory.systemUpdate.openBtn') }}
                </el-button>
            </div>
        </div>

        <!-- 守护进程：本地引擎的唯一重型进程，承载 .zcsr 温层与索引快照。 -->
        <el-card shadow="never" class="mt-16 sys-card" data-testid="card-system-daemon">
            <div class="sec-head">
                <span class="sec-title">{{ t('plugin.rolling-memory.system.daemonTitle') }}</span>
                <el-tag
                    size="small"
                    :type="daemonOnline ? 'success' : 'info'"
                    effect="dark"
                    data-testid="badge-daemon-status"
                >
                    {{ daemonOnline ? t('plugin.rolling-memory.system.running') : t('plugin.rolling-memory.system.stopped') }}
                </el-tag>
                <span class="sys-sub">{{ t('plugin.rolling-memory.system.daemonSub') }}</span>
            </div>
            <div class="sys-grid" data-testid="daemon-metrics">
                <div class="sys-item">
                    <span class="sys-num">{{ daemonOnline ? daemon?.pid ?? '—' : '—' }}</span>
                    <span class="sys-lbl">{{ t('plugin.rolling-memory.system.pidLbl') }}</span>
                </div>
                <div class="sys-item">
                    <span class="sys-num">{{ daemonOnline ? formatUptime(daemon?.uptime_sec) : '—' }}</span>
                    <span class="sys-lbl">{{ t('plugin.rolling-memory.system.uptimeLbl') }}</span>
                </div>
                <div class="sys-item">
                    <span class="sys-num">{{ daemonOnline ? formatBytes(daemon?.heap_alloc_bytes) : '—' }}</span>
                    <span class="sys-lbl">{{ t('plugin.rolling-memory.system.heapLbl') }}</span>
                </div>
                <div class="sys-item">
                    <span class="sys-num">{{ effectiveLimitText }}</span>
                    <span class="sys-lbl">{{ t('plugin.rolling-memory.system.limitLbl') }}</span>
                </div>
                <div class="sys-item">
                    <span class="sys-num">{{ limitSourceText }}</span>
                    <span class="sys-lbl">{{ t('plugin.rolling-memory.system.sourceLbl') }}</span>
                </div>
            </div>
            <div class="sys-actions">
                <el-button
                    v-if="!daemonOnline"
                    type="primary"
                    size="small"
                    :loading="daemonBusy === 'start'"
                    data-testid="btn-daemon-start"
                    @click="runDaemon('start')"
                >
                    {{ t('plugin.rolling-memory.system.startBtn') }}
                </el-button>
                <el-button
                    type="warning"
                    size="small"
                    plain
                    :loading="daemonBusy === 'restart'"
                    data-testid="btn-daemon-restart"
                    @click="runDaemon('restart')"
                >
                    {{ t('plugin.rolling-memory.system.restartBtn') }}
                </el-button>
                <el-button
                    type="danger"
                    size="small"
                    plain
                    :disabled="!daemonOnline"
                    :loading="daemonBusy === 'stop'"
                    data-testid="btn-daemon-stop"
                    @click="runDaemon('stop')"
                >
                    {{ t('plugin.rolling-memory.system.stopBtn') }}
                </el-button>
                <span class="sys-hint">{{ t('plugin.rolling-memory.system.daemonHint') }}</span>
            </div>
        </el-card>

        <!-- 内存上限：Go 软内存上限（GOMEMLIMIT 语义），重启守护进程后生效。 -->
        <el-card shadow="never" class="mt-16 sys-card" data-testid="card-system-memory">
            <div class="sec-head">
                <span class="sec-title">{{ t('plugin.rolling-memory.system.memoryTitle') }}</span>
                <span class="sys-sub">{{ t('plugin.rolling-memory.system.memorySub') }}</span>
            </div>
            <div class="sys-actions">
                <el-select
                    v-model="memoryLimit"
                    size="small"
                    filterable
                    allow-create
                    default-first-option
                    style="width: 220px"
                    data-testid="select-memory-limit"
                >
                    <el-option :label="t('plugin.rolling-memory.system.memoryAutoOption')" value="auto" />
                    <el-option :label="t('plugin.rolling-memory.system.memoryOffOption')" value="off" />
                    <el-option label="512MiB" value="512MiB" />
                    <el-option label="1GiB" value="1GiB" />
                    <el-option label="2GiB" value="2GiB" />
                    <el-option label="4GiB" value="4GiB" />
                </el-select>
                <el-button
                    type="primary"
                    size="small"
                    :loading="memorySaving"
                    :disabled="!memoryLimit || memoryLimit === configuredLimit"
                    data-testid="btn-memory-save"
                    @click="saveMemoryLimit"
                >
                    {{ t('plugin.rolling-memory.system.saveBtn') }}
                </el-button>
                <span class="sys-hint">{{ t('plugin.rolling-memory.system.memoryHint') }}</span>
            </div>
            <div class="sys-grid" data-testid="memory-metrics">
                <div class="sys-item">
                    <span class="sys-num">{{ configuredLimit || '—' }}</span>
                    <span class="sys-lbl">{{ t('plugin.rolling-memory.system.memoryConfigLbl') }}</span>
                </div>
                <div class="sys-item">
                    <span class="sys-num">{{ effectiveLimitText }}</span>
                    <span class="sys-lbl">{{ t('plugin.rolling-memory.system.memoryEffectiveLbl') }}</span>
                </div>
            </div>
        </el-card>

        <!-- 缓存：内存快照与读缓存均可再生成，清理后由下一次召回自动预热。 -->
        <el-card shadow="never" class="mt-16 sys-card" data-testid="card-system-cache">
            <div class="sec-head">
                <span class="sec-title">{{ t('plugin.rolling-memory.system.cacheTitle') }}</span>
                <span class="sys-sub">{{ t('plugin.rolling-memory.system.cacheSub') }}</span>
            </div>
            <div class="sys-actions">
                <el-button size="small" :loading="cacheBusy" data-testid="btn-cache-clear" @click="clearCache">
                    {{ t('plugin.rolling-memory.system.clearBtn') }}
                </el-button>
                <span class="sys-hint">{{ t('plugin.rolling-memory.system.cacheHint') }}</span>
            </div>
        </el-card>

        <!-- 磁盘维护：扫描为只读体检；清理与瘦身走产品 CLI `maintain` 正典路径。 -->
        <el-card shadow="never" class="mt-16 sys-card" data-testid="card-system-space">
            <div class="sec-head">
                <span class="sec-title">{{ t('plugin.rolling-memory.system.spaceTitle') }}</span>
                <span class="sys-sub">{{ t('plugin.rolling-memory.system.spaceSub') }}</span>
            </div>
            <div class="sys-actions">
                <el-button size="small" :loading="spaceBusy === 'status'" data-testid="btn-system-space-scan" @click="handleSpace('status')">
                    {{ t('plugin.rolling-memory.system.scanBtn') }}
                </el-button>
                <el-button
                    size="small"
                    type="warning"
                    plain
                    :loading="spaceBusy === 'clean'"
                    :disabled="!spaceCleanActionable"
                    data-testid="btn-system-space-clean"
                    @click="handleSpace('clean')"
                >
                    {{ t('plugin.rolling-memory.system.cleanBtn') }}
                </el-button>
                <el-button
                    size="small"
                    type="danger"
                    plain
                    :loading="spaceBusy === 'compact'"
                    :disabled="!spaceActionable"
                    data-testid="btn-system-space-compact"
                    @click="handleSpace('compact')"
                >
                    {{ t('plugin.rolling-memory.system.compactBtn') }}
                </el-button>
            </div>
            <div v-if="spaceReport" class="sys-grid" data-testid="system-space-report">
                <div class="sys-item">
                    <span class="sys-num">{{ formatBytes(spaceReclaimable) }}</span>
                    <span class="sys-lbl">{{ t('plugin.rolling-memory.system.totalLbl') }}</span>
                </div>
                <div class="sys-item">
                    <span class="sys-num">{{ formatBytes(spaceBackupsReclaim) }}</span>
                    <span class="sys-lbl">{{ t('plugin.rolling-memory.system.backupsLbl') }}</span>
                </div>
                <div class="sys-item">
                    <span class="sys-num">{{ formatBytes(spaceCachesReclaim) }}</span>
                    <span class="sys-lbl">{{ t('plugin.rolling-memory.system.cachesLbl') }}</span>
                </div>
                <div class="sys-item">
                    <span class="sys-num">{{ formatBytes(spaceCompactReclaim) }}</span>
                    <span class="sys-lbl">{{ t('plugin.rolling-memory.system.compactLbl') }}</span>
                </div>
                <div v-if="spaceFreed > 0" class="sys-item">
                    <span class="sys-num">{{ formatBytes(spaceFreed) }}</span>
                    <span class="sys-lbl">{{ t('plugin.rolling-memory.system.freedLbl') }}</span>
                </div>
            </div>
        </el-card>

        <systemUpdateDialog v-model="showSystemUpdateDialog" />
    </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import request from '@/utils/request'
import systemUpdateDialog from './systemUpdateDialog.vue'

defineOptions({
    name: 'plugin/rolling-memory/system',
})

const { t } = useI18n()

const daemon = ref<any>(null)
const daemonOnline = computed(() => Boolean(daemon.value?.online))
const daemonBusy = ref('')
const memoryLimit = ref('auto')
const memorySaving = ref(false)
const cacheBusy = ref(false)
const spaceBusy = ref('')
const spaceReport = ref<any>(null)
const showSystemUpdateDialog = ref(false)

const configuredLimit = computed(() => String(daemon.value?.configured_memory_limit || ''))

// 生效上限：守护在线时以运行时上报为准；离线时显示占位，不把配置值伪装成生效值。
const effectiveLimitText = computed(() => {
    if (!daemonOnline.value) return '—'
    const source = String(daemon.value?.mem_limit_source || '')
    if (source === 'off') return t('plugin.rolling-memory.system.memoryOffOption')
    const bytes = Number(daemon.value?.mem_limit_bytes || 0)
    return bytes > 0 ? formatBytes(bytes) : '—'
})

const limitSourceText = computed(() => {
    const source = String(daemon.value?.mem_limit_source || '')
    switch (source) {
        case 'auto':
            return t('plugin.rolling-memory.system.sourceAuto')
        case 'explicit':
            return t('plugin.rolling-memory.system.sourceExplicit')
        case 'env':
            return t('plugin.rolling-memory.system.sourceEnv')
        case 'off':
            return t('plugin.rolling-memory.system.sourceOff')
        case 'unavailable':
            return t('plugin.rolling-memory.system.sourceUnavailable')
        default:
            return '—'
    }
})

function formatBytes(value: unknown): string {
    const bytes = Number(value ?? 0)
    if (!bytes) return '0 B'
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
    return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
}

function formatUptime(value: unknown): string {
    const seconds = Number(value ?? 0)
    if (!seconds) return '0s'
    const hours = Math.floor(seconds / 3600)
    const minutes = Math.floor((seconds % 3600) / 60)
    if (hours > 0) return `${hours}h ${minutes}m`
    if (minutes > 0) return `${minutes}m ${seconds % 60}s`
    return `${seconds}s`
}

const loadDaemon = async () => {
    if (daemonBusy.value) return
    daemonBusy.value = 'status'
    try {
        const res = await request<any>({
            url: '/admin/plugin/rolling-memory/system/daemon',
            method: 'POST',
            data: { action: 'status' },
            timeout: 0,
        })
        daemon.value = res.data?.data?.report || null
        const configured = String(daemon.value?.configured_memory_limit || '')
        if (configured) memoryLimit.value = configured
    } catch {
        // 失败提示由 request 统一弹出；保留上次读数。
    } finally {
        daemonBusy.value = ''
    }
}

const runDaemon = async (action: 'start' | 'stop' | 'restart') => {
    if (daemonBusy.value) return
    if (action === 'stop' || action === 'restart') {
        const key = action === 'stop' ? 'stopConfirm' : 'restartConfirm'
        try {
            await ElMessageBox.confirm(t(`plugin.rolling-memory.system.${key}`), t('plugin.rolling-memory.system.confirmTitle'), {
                type: 'warning',
            })
        } catch {
            return
        }
    }
    daemonBusy.value = action
    try {
        const res = await request<any>({
            url: '/admin/plugin/rolling-memory/system/daemon',
            method: 'POST',
            data: { action },
            timeout: 0,
        })
        daemon.value = res.data?.data?.report || null
        ElMessage.success(t('plugin.rolling-memory.system.actionDoneToast'))
    } catch {
        // 错误提示由 request 统一弹出。
    } finally {
        daemonBusy.value = ''
        void loadDaemon()
    }
}

const saveMemoryLimit = async () => {
    if (memorySaving.value || !memoryLimit.value) return
    memorySaving.value = true
    try {
        const res = await request<any>({
            url: '/admin/plugin/rolling-memory/system/memory-limit',
            method: 'POST',
            data: { value: memoryLimit.value },
            timeout: 0,
        })
        const saved = String(res.data?.data?.memory_limit || memoryLimit.value)
        memoryLimit.value = saved
        ElMessage.success(t('plugin.rolling-memory.system.savedToast'))
        if (res.data?.data?.restart_required) {
            try {
                await ElMessageBox.confirm(t('plugin.rolling-memory.system.restartNowConfirm'), t('plugin.rolling-memory.system.confirmTitle'), {
                    type: 'info',
                    confirmButtonText: t('plugin.rolling-memory.system.restartBtn'),
                    cancelButtonText: t('plugin.rolling-memory.system.restartLater'),
                })
                await runDaemon('restart')
            } catch {
                // 用户选择稍后重启：配置已落盘，下次启动生效。
            }
        }
    } catch {
        // 错误提示由 request 统一弹出。
    } finally {
        memorySaving.value = false
    }
}

const clearCache = async () => {
    if (cacheBusy.value) return
    cacheBusy.value = true
    try {
        await request<any>({
            url: '/admin/plugin/rolling-memory/system/cache',
            method: 'POST',
            data: {},
            timeout: 0,
        })
        ElMessage.success(t('plugin.rolling-memory.system.clearedToast'))
    } catch {
        // 错误提示由 request 统一弹出。
    } finally {
        cacheBusy.value = false
    }
}

const requestSpace = async (action: 'status' | 'clean' | 'compact', apply = false) => {
    const res = await request<any>({
        url: '/admin/plugin/rolling-memory/maintenance/space',
        method: 'POST',
        data: { action, apply, confirm: apply, caches: action === 'clean' },
        timeout: 0,
    })
    spaceReport.value = res.data?.data?.report || null
    return spaceReport.value
}

const handleSpace = async (action: 'status' | 'clean' | 'compact') => {
    if (spaceBusy.value) return
    const apply = action !== 'status'
    if (apply) {
        const key = action === 'clean' ? 'cleanConfirm' : 'compactConfirm'
        try {
            await ElMessageBox.confirm(t(`plugin.rolling-memory.system.${key}`), t('plugin.rolling-memory.system.confirmTitle'), {
                type: 'warning',
            })
        } catch {
            return
        }
    }
    spaceBusy.value = action
    try {
        await requestSpace(action, apply)
        if (apply) {
            ElMessage.success(t('plugin.rolling-memory.system.doneToast'))
            void loadDaemon()
        }
    } catch {
        // 错误提示由 request 统一弹出。
    } finally {
        spaceBusy.value = ''
    }
}

// 磁盘读数口径与「记忆总览」的空间维护卡一致（与服务端报告字段逐项对应）。
const spaceReclaimable = computed(() => Number(spaceReport.value?.reclaimable?.total_bytes || 0))
const spaceBackupsReclaim = computed(() => Number(spaceReport.value?.reclaimable?.backups_bytes || 0))
const spaceCachesReclaim = computed(() => Number(spaceReport.value?.reclaimable?.caches_bytes || 0))
const spaceCompactReclaim = computed(() => Number(spaceReport.value?.reclaimable?.compact_bytes || 0))
const spaceFreed = computed(() => Number(spaceReport.value?.freed_bytes || 0))
const spaceActionable = computed(() => spaceReport.value !== null && spaceCompactReclaim.value > 0)
const spaceCleanActionable = computed(
    () => spaceReport.value !== null && spaceBackupsReclaim.value + spaceCachesReclaim.value > 0
)

onMounted(() => {
    void loadDaemon()
})
</script>

<style scoped>
.sys-hero {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
    padding: 18px 20px;
    border-radius: 12px;
    background: linear-gradient(120deg, var(--el-fill-color-light), var(--el-bg-color));
    border: 1px solid var(--el-border-color-lighter);
}
.sys-hero-titles {
    display: flex;
    flex-direction: column;
    gap: 6px;
}
.sys-hero-title {
    font-size: 20px;
    font-weight: 600;
    color: var(--el-text-color-primary);
}
.sys-hero-sub {
    font-size: 13px;
    color: var(--el-text-color-secondary);
}
.sys-hero-actions {
    display: flex;
    align-items: center;
    gap: 8px;
}
.sys-card {
    border-radius: 12px;
}
.sec-head {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
}
.sec-title {
    font-size: 15px;
    font-weight: 600;
    color: var(--el-text-color-primary);
}
.sys-sub {
    font-size: 12px;
    color: var(--el-text-color-secondary);
}
.sys-grid {
    display: flex;
    flex-wrap: wrap;
    gap: 24px;
    margin-top: 14px;
}
.sys-item {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 120px;
}
.sys-num {
    font-size: 18px;
    font-weight: 600;
    color: var(--el-text-color-primary);
}
.sys-lbl {
    font-size: 12px;
    color: var(--el-text-color-secondary);
}
.sys-actions {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 10px;
    margin-top: 14px;
}
.sys-hint {
    font-size: 12px;
    color: var(--el-text-color-secondary);
}
</style>
