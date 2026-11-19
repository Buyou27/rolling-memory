<!-- plugin: rolling-memory system management (operations dashboard) -->
<template>
    <div class="default-main plugin-rolling-memory system-page">
        <!-- Hero：身份、实时通道状态与全局动作 -->
        <section class="sys-hero" :class="heroTone">
            <div class="sys-hero-left">
                <div class="sys-hero-badge">
                    <Icon name="lucide-server-cog" :size="22" />
                </div>
                <div class="sys-hero-titles">
                    <div class="sys-hero-title-row">
                        <span class="sys-hero-title">{{ t('plugin.rolling-memory.system.title') }}</span>
                        <el-tag size="small" effect="plain" class="sys-mode-pill">{{ t('plugin.rolling-memory.system.modePill') }}</el-tag>
                        <el-tag size="small" :type="statusTagType" effect="dark" data-testid="badge-daemon-status">
                            <span class="sys-status-dot" :class="statusDotClass" />
                            {{ statusText }}
                        </el-tag>
                        <el-tag v-if="stale" size="small" type="info" effect="plain" data-testid="badge-stale">
                            {{ t('plugin.rolling-memory.system.staleBadge') }}
                        </el-tag>
                    </div>
                    <span class="sys-hero-sub">{{ t('plugin.rolling-memory.system.sub') }}</span>
                </div>
            </div>
            <div class="sys-hero-right">
                <div class="sys-live" :class="`is-${liveState}`" data-testid="live-indicator">
                    <span class="sys-live-dot" />
                    <span class="sys-live-label">{{ liveLabel }}</span>
                    <span v-if="lastUpdatedText" class="sys-live-time">{{ t('plugin.rolling-memory.system.lastUpdatedLbl') }} {{ lastUpdatedText }}</span>
                </div>
                <el-button size="small" :loading="refreshing" data-testid="btn-system-refresh" @click="refreshNow">
                    {{ t('plugin.rolling-memory.system.refreshBtn') }}
                </el-button>
                <el-button size="small" plain data-testid="btn-system-update" @click="showSystemUpdateDialog = true">
                    <span v-if="updateState?.update_available" class="rm-update-dot" data-testid="dot-system-update" />
                    {{ t('plugin.rolling-memory.systemUpdate.openBtn') }}
                </el-button>
            </div>
        </section>

        <!-- KPI 四卡：运行时长 / 堆内存 / 进程内存 / 生效上限 -->
        <div class="sys-kpis">
            <div class="sys-kpi" data-testid="kpi-uptime">
                <div class="sys-kpi-head">
                    <span>{{ t('plugin.rolling-memory.system.uptimeLbl') }}</span>
                    <Icon name="lucide-timer" :size="16" />
                </div>
                <el-skeleton v-if="!daemon" :rows="1" animated class="sys-kpi-skeleton" />
                <template v-else>
                    <div class="sys-kpi-num">{{ daemonOnline ? formatUptime(liveUptime) : '—' }}</div>
                    <div class="sys-kpi-sub">
                        <template v-if="daemonOnline">
                            {{ t('plugin.rolling-memory.system.pidLbl') }} {{ daemon.pid ?? '—' }}
                            <span v-if="startedAtText" class="sys-sep">·</span>
                            <span v-if="startedAtText">{{ t('plugin.rolling-memory.system.startedAtLbl') }} {{ startedAtText }}</span>
                        </template>
                        <template v-else>{{ t('plugin.rolling-memory.system.kpiNotRunning') }}</template>
                    </div>
                </template>
            </div>

            <div class="sys-kpi" :class="heapToneClass" data-testid="kpi-heap">
                <div class="sys-kpi-head">
                    <span>{{ t('plugin.rolling-memory.system.heapLbl') }}</span>
                    <Icon name="lucide-cpu" :size="16" />
                </div>
                <el-skeleton v-if="!daemon" :rows="1" animated class="sys-kpi-skeleton" />
                <template v-else>
                    <div class="sys-kpi-num">{{ daemonOnline ? formatBytes(daemon.heap_alloc_bytes) : '—' }}</div>
                    <el-progress
                        v-if="daemonOnline && heapLimitPct !== null"
                        class="sys-kpi-progress"
                        :percentage="heapProgressPct"
                        :stroke-width="8"
                        :show-text="false"
                        :color="heapProgressColor"
                    />
                    <div class="sys-kpi-sub">
                        <template v-if="daemonOnline && heapLimitPct !== null">
                            {{ t('plugin.rolling-memory.system.kpiHeapOfLimit') }} {{ heapLimitPct.toFixed(1) }}%
                        </template>
                        <template v-else>—</template>
                    </div>
                </template>
            </div>

            <div class="sys-kpi" data-testid="kpi-sysmem">
                <div class="sys-kpi-head">
                    <span>{{ t('plugin.rolling-memory.system.kpiSysMemLbl') }}</span>
                    <Icon name="lucide-activity" :size="16" />
                </div>
                <el-skeleton v-if="!daemon" :rows="1" animated class="sys-kpi-skeleton" />
                <template v-else>
                    <div class="sys-kpi-num">{{ daemonOnline ? formatBytes(daemon.heap_sys_bytes) : '—' }}</div>
                    <div class="sys-kpi-sub">
                        <template v-if="daemonOnline && ramPct !== null">
                            {{ t('plugin.rolling-memory.system.kpiSysOfRam') }} {{ ramPct.toFixed(1) }}%
                            <span v-if="ramText" class="sys-sep">·</span>
                            <span v-if="ramText">{{ ramText }}</span>
                        </template>
                        <template v-else>—</template>
                    </div>
                </template>
            </div>

            <div class="sys-kpi" data-testid="kpi-limit">
                <div class="sys-kpi-head">
                    <span>{{ t('plugin.rolling-memory.system.limitLbl') }}</span>
                    <Icon name="lucide-gauge" :size="16" />
                </div>
                <el-skeleton v-if="!daemon" :rows="1" animated class="sys-kpi-skeleton" />
                <template v-else>
                    <div class="sys-kpi-num">{{ effectiveLimitText }}</div>
                    <div class="sys-kpi-sub">
                        <el-tag size="small" :type="limitSourceTagType" effect="plain">{{ limitSourceText }}</el-tag>
                        <span class="sys-kpi-sub-text">{{ t('plugin.rolling-memory.system.memoryConfigLbl') }} {{ configuredLimit || '—' }}</span>
                    </div>
                </template>
            </div>
        </div>

        <div class="sys-main">
            <div class="sys-col-main">
                <!-- 守护进程 -->
                <section class="sys-card" data-testid="card-system-daemon">
                    <header class="sys-card-head">
                        <div class="sys-card-head-main">
                            <span class="sys-card-title">{{ t('plugin.rolling-memory.system.daemonTitle') }}</span>
                            <span class="sys-card-sub">{{ t('plugin.rolling-memory.system.daemonSub') }}</span>
                        </div>
                        <div class="sys-card-actions">
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
                                v-else
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
                        </div>
                    </header>

                    <el-alert
                        v-if="daemonError"
                        class="sys-alert"
                        type="error"
                        :title="t('plugin.rolling-memory.system.healthErrorTitle')"
                        :description="daemonError"
                        :closable="false"
                        show-icon
                        data-testid="daemon-health-error"
                    >
                        <template #default>
                            <div class="sys-alert-body">
                                <span class="sys-alert-text">{{ daemonError }}</span>
                                <el-button size="small" text type="primary" :loading="refreshing" @click="refreshNow">
                                    {{ t('plugin.rolling-memory.system.retryBtn') }}
                                </el-button>
                            </div>
                        </template>
                    </el-alert>

                    <el-skeleton v-if="!daemon" :rows="4" animated />
                    <div v-else-if="!daemonOnline && !daemonError" class="sys-empty" data-testid="daemon-offline-empty">
                        <Icon name="lucide-server-off" :size="34" />
                        <div class="sys-empty-title">{{ t('plugin.rolling-memory.system.offlineEmptyTitle') }}</div>
                        <div class="sys-empty-desc">{{ t('plugin.rolling-memory.system.offlineEmptyDesc') }}</div>
                        <el-button type="primary" size="small" :loading="daemonBusy === 'start'" @click="runDaemon('start')">
                            {{ t('plugin.rolling-memory.system.startBtn') }}
                        </el-button>
                    </div>
                    <template v-else-if="daemonOnline">
                        <el-descriptions class="sys-desc" :column="2" border size="small">
                            <el-descriptions-item :label="t('plugin.rolling-memory.system.pidLbl')">
                                <span class="sys-mono">{{ daemon.pid ?? '—' }}</span>
                            </el-descriptions-item>
                            <el-descriptions-item :label="t('plugin.rolling-memory.system.startedAtLbl')">
                                <span class="sys-mono">{{ startedAtText || '—' }}</span>
                            </el-descriptions-item>
                            <el-descriptions-item :label="t('plugin.rolling-memory.system.socketLbl')">
                                <span class="sys-mono sys-ellipsis" :title="daemon.socket">{{ daemon.socket || '—' }}</span>
                            </el-descriptions-item>
                            <el-descriptions-item :label="t('plugin.rolling-memory.system.dbPathLbl')">
                                <span class="sys-mono sys-ellipsis" :title="daemon.db">{{ daemon.db || '—' }}</span>
                            </el-descriptions-item>
                            <el-descriptions-item :label="t('plugin.rolling-memory.system.configPathLbl')">
                                <span class="sys-mono sys-ellipsis" :title="daemon.config">{{ daemon.config || '—' }}</span>
                            </el-descriptions-item>
                            <el-descriptions-item :label="t('plugin.rolling-memory.system.configMatchLbl')">
                                <el-tag size="small" :type="daemon.config_matches ? 'success' : 'danger'" effect="plain">
                                    {{
                                        daemon.config_matches
                                            ? t('plugin.rolling-memory.system.configMatchOk')
                                            : t('plugin.rolling-memory.system.configMatchMismatch')
                                    }}
                                </el-tag>
                            </el-descriptions-item>
                        </el-descriptions>
                        <p class="sys-hint">{{ t('plugin.rolling-memory.system.daemonHint') }}</p>
                    </template>
                </section>

                <!-- 内存上限 -->
                <section class="sys-card" data-testid="card-system-memory">
                    <header class="sys-card-head">
                        <div class="sys-card-head-main">
                            <span class="sys-card-title">{{ t('plugin.rolling-memory.system.memoryTitle') }}</span>
                            <span class="sys-card-sub">{{ t('plugin.rolling-memory.system.memorySub') }}</span>
                        </div>
                        <div class="sys-card-actions">
                            <el-tag v-if="memoryDirty" size="small" type="warning" effect="plain">
                                {{ t('plugin.rolling-memory.system.memoryDirtyHint') }}
                            </el-tag>
                            <el-button
                                type="primary"
                                size="small"
                                :loading="memorySaving"
                                :disabled="!memoryDirty || !memoryLimit"
                                data-testid="btn-memory-save"
                                @click="saveMemoryLimit"
                            >
                                {{ t('plugin.rolling-memory.system.saveBtn') }}
                            </el-button>
                        </div>
                    </header>

                    <div class="sys-preset-row">
                        <span class="sys-preset-label">{{ t('plugin.rolling-memory.system.presetLbl') }}</span>
                        <el-radio-group v-model="selectedPreset" size="small" @change="onPresetChange">
                            <el-radio-button value="auto">{{ t('plugin.rolling-memory.system.memoryAutoOption') }}</el-radio-button>
                            <el-radio-button value="off">{{ t('plugin.rolling-memory.system.memoryOffOption') }}</el-radio-button>
                            <el-radio-button value="512MiB">512MiB</el-radio-button>
                            <el-radio-button value="1GiB">1GiB</el-radio-button>
                            <el-radio-button value="2GiB">2GiB</el-radio-button>
                            <el-radio-button value="4GiB">4GiB</el-radio-button>
                            <el-radio-button value="custom">{{ t('plugin.rolling-memory.system.presetCustom') }}</el-radio-button>
                        </el-radio-group>
                        <el-input
                            v-if="selectedPreset === 'custom'"
                            v-model="customLimit"
                            size="small"
                            class="sys-custom-limit"
                            :placeholder="t('plugin.rolling-memory.system.customPlaceholder')"
                            data-testid="input-custom-limit"
                            @input="onCustomLimitInput"
                        />
                    </div>

                    <div class="sys-compare">
                        <div class="sys-compare-item">
                            <span class="sys-compare-lbl">{{ t('plugin.rolling-memory.system.memoryConfigLbl') }}</span>
                            <span class="sys-compare-val sys-mono">{{ configuredLimit || '—' }}</span>
                        </div>
                        <Icon name="lucide-arrow-right" :size="16" class="sys-compare-arrow" />
                        <div class="sys-compare-item">
                            <span class="sys-compare-lbl">{{ t('plugin.rolling-memory.system.memoryEffectiveLbl') }}</span>
                            <span class="sys-compare-val sys-mono">{{ effectiveLimitText }}</span>
                        </div>
                        <el-tag size="small" :type="limitSourceTagType" effect="plain">{{ limitSourceText }}</el-tag>
                    </div>

                    <el-alert
                        v-if="envOverrideActive"
                        class="sys-alert"
                        type="info"
                        :closable="false"
                        show-icon
                        :title="t('plugin.rolling-memory.system.envOverrideTitle')"
                        :description="t('plugin.rolling-memory.system.envOverrideDesc', { value: envOverrideValue })"
                        data-testid="memory-env-override"
                    />

                    <el-alert
                        v-if="restartPending"
                        class="sys-alert"
                        type="warning"
                        :closable="false"
                        show-icon
                        data-testid="memory-restart-banner"
                    >
                        <template #default>
                            <div class="sys-alert-body">
                                <span class="sys-alert-text">{{ t('plugin.rolling-memory.system.restartBanner') }}</span>
                                <el-button
                                    size="small"
                                    type="warning"
                                    :loading="daemonBusy === 'restart'"
                                    data-testid="btn-memory-restart-now"
                                    @click="applyMemoryRestart"
                                >
                                    {{ t('plugin.rolling-memory.system.restartNowBtn') }}
                                </el-button>
                            </div>
                        </template>
                    </el-alert>
                    <p class="sys-hint">{{ t('plugin.rolling-memory.system.memoryHint') }}</p>
                </section>

                <!-- 磁盘维护 -->
                <section class="sys-card" data-testid="card-system-space">
                    <header class="sys-card-head">
                        <div class="sys-card-head-main">
                            <span class="sys-card-title">{{ t('plugin.rolling-memory.system.spaceTitle') }}</span>
                            <span class="sys-card-sub">{{ t('plugin.rolling-memory.system.spaceSub') }}</span>
                        </div>
                        <div class="sys-card-actions">
                            <span class="sys-keep-label">{{ t('plugin.rolling-memory.system.keepLbl') }}</span>
                            <el-select v-model="keepBackups" size="small" class="sys-keep-select" data-testid="select-keep-backups">
                                <el-option :label="t('plugin.rolling-memory.system.keepDefault')" :value="0" />
                                <el-option
                                    v-for="n in 20"
                                    :key="n"
                                    :label="t('plugin.rolling-memory.system.keepOption', { n })"
                                    :value="n"
                                />
                            </el-select>
                            <el-button size="small" :loading="spaceBusy === 'status'" data-testid="btn-system-space-scan" @click="handleScan">
                                {{ t('plugin.rolling-memory.system.scanBtn') }}
                            </el-button>
                            <el-button
                                size="small"
                                type="warning"
                                plain
                                :loading="spaceBusy === 'clean'"
                                :disabled="!spaceCleanActionable"
                                data-testid="btn-system-space-clean"
                                @click="handleApply('clean')"
                            >
                                {{ t('plugin.rolling-memory.system.cleanBtn') }}
                            </el-button>
                            <el-button
                                size="small"
                                type="danger"
                                plain
                                :loading="spaceBusy === 'compact'"
                                :disabled="!spaceCompactActionable"
                                data-testid="btn-system-space-compact"
                                @click="handleApply('compact')"
                            >
                                {{ t('plugin.rolling-memory.system.compactBtn') }}
                            </el-button>
                        </div>
                    </header>

                    <div v-if="!spaceScan" class="sys-empty sys-empty-inline" data-testid="space-scan-empty">
                        <Icon name="lucide-hard-drive" :size="26" />
                        <span class="sys-empty-desc">{{ t('plugin.rolling-memory.system.scanFirstHint') }}</span>
                    </div>
                    <template v-else>
                        <div class="sys-metrics" data-testid="system-space-report">
                            <div class="sys-metric is-accent">
                                <span class="sys-metric-num">{{ formatBytes(spaceTotal) }}</span>
                                <span class="sys-metric-lbl">{{ t('plugin.rolling-memory.system.totalLbl') }}</span>
                            </div>
                            <div class="sys-metric">
                                <span class="sys-metric-num">{{ formatBytes(spaceBackups) }}</span>
                                <span class="sys-metric-lbl">{{ t('plugin.rolling-memory.system.backupsLbl') }}</span>
                            </div>
                            <div class="sys-metric">
                                <span class="sys-metric-num">{{ formatBytes(spaceCaches) }}</span>
                                <span class="sys-metric-lbl">{{ t('plugin.rolling-memory.system.cachesLbl') }}</span>
                            </div>
                            <div class="sys-metric">
                                <span class="sys-metric-num">{{ formatBytes(spaceCompact) }}</span>
                                <span class="sys-metric-lbl">{{ t('plugin.rolling-memory.system.compactLbl') }}</span>
                            </div>
                            <div class="sys-metric">
                                <span class="sys-metric-num">{{ formatBytes(vaultBytes) }}</span>
                                <span class="sys-metric-lbl">{{ t('plugin.rolling-memory.system.vaultSizeLbl') }}</span>
                            </div>
                            <div class="sys-metric">
                                <span class="sys-metric-num">{{ formatBytes(logsBytes) }}</span>
                                <span class="sys-metric-lbl">{{ t('plugin.rolling-memory.system.logsLbl') }}</span>
                            </div>
                        </div>

                        <div v-if="spaceStack.length" class="sys-stack" data-testid="space-stack">
                            <span
                                v-for="seg in spaceStack"
                                :key="seg.key"
                                class="sys-stack-seg"
                                :class="`is-${seg.key}`"
                                :style="{ flexGrow: seg.bytes }"
                                :title="`${seg.label}: ${formatBytes(seg.bytes)}`"
                            />
                        </div>
                        <div v-if="spaceStack.length" class="sys-stack-legend">
                            <span v-for="seg in spaceStack" :key="seg.key" class="sys-legend-item">
                                <span class="sys-legend-dot" :class="`is-${seg.key}`" />{{ seg.label }} {{ formatBytes(seg.bytes) }}
                            </span>
                        </div>

                        <div v-if="lastApply" class="sys-apply-result" data-testid="space-apply-result">
                            <span>
                                {{ t('plugin.rolling-memory.system.freedLbl') }}
                                <strong>{{ formatBytes(lastApply.freed_bytes) }}</strong>
                            </span>
                            <span v-if="lastApply.receipt" class="sys-receipt">
                                {{ t('plugin.rolling-memory.system.receiptLbl') }}: <code>{{ lastApply.receipt }}</code>
                                <el-button size="small" text type="primary" @click="copyText(String(lastApply.receipt))">
                                    {{ t('plugin.rolling-memory.system.copyBtn') }}
                                </el-button>
                            </span>
                        </div>

                        <el-collapse class="sys-collapse">
                            <el-collapse-item :title="t('plugin.rolling-memory.system.detailTitle')" name="details">
                                <div class="sys-detail-grid">
                                    <div class="sys-detail-block">
                                        <div class="sys-detail-title">{{ t('plugin.rolling-memory.system.backupsItemsLbl') }}</div>
                                        <div class="sys-detail-row">
                                            <span>{{ t('plugin.rolling-memory.system.itemsCount', { n: spaceItems('backups') }) }}</span>
                                            <span class="sys-mono">{{ formatBytes(spaceSectionBytes('backups')) }}</span>
                                        </div>
                                    </div>
                                    <div class="sys-detail-block">
                                        <div class="sys-detail-title">{{ t('plugin.rolling-memory.system.cachesItemsLbl') }}</div>
                                        <div class="sys-detail-row">
                                            <span>{{ t('plugin.rolling-memory.system.itemsCount', { n: spaceItems('caches') }) }}</span>
                                            <span class="sys-mono">{{ formatBytes(spaceSectionBytes('caches')) }}</span>
                                        </div>
                                    </div>
                                    <div class="sys-detail-block">
                                        <div class="sys-detail-title">{{ t('plugin.rolling-memory.system.logsItemsLbl') }}</div>
                                        <div class="sys-detail-row">
                                            <span>{{ t('plugin.rolling-memory.system.itemsCount', { n: spaceItems('logs') }) }}</span>
                                            <span class="sys-mono">{{ formatBytes(spaceSectionBytes('logs')) }}</span>
                                        </div>
                                    </div>
                                    <div class="sys-detail-block">
                                        <div class="sys-detail-title">{{ t('plugin.rolling-memory.system.vaultDetailTitle') }}</div>
                                        <div class="sys-detail-row">
                                            <span>db</span>
                                            <span class="sys-mono">{{ formatBytes(spaceScan?.vault?.db_bytes) }}</span>
                                        </div>
                                        <div class="sys-detail-row">
                                            <span>wal / sidecar</span>
                                            <span class="sys-mono">
                                                {{ formatBytes(spaceScan?.vault?.wal_bytes) }} / {{ formatBytes(spaceScan?.vault?.sidecar_bytes) }}
                                            </span>
                                        </div>
                                    </div>
                                    <div class="sys-detail-block">
                                        <div class="sys-detail-title">{{ t('plugin.rolling-memory.system.keepPolicyLbl') }}</div>
                                        <div class="sys-detail-row">
                                            <span>{{ t('plugin.rolling-memory.system.keepLbl') }}</span>
                                            <span class="sys-mono">{{ spaceScan?.keep_policy?.backup_keep_per_family ?? '—' }}</span>
                                        </div>
                                    </div>
                                    <div class="sys-detail-block">
                                        <div class="sys-detail-title">{{ t('plugin.rolling-memory.system.receiptLbl') }}</div>
                                        <div class="sys-detail-row">
                                            <code class="sys-ellipsis" :title="spaceScan?.receipt">{{ spaceScan?.receipt || '—' }}</code>
                                            <el-button v-if="spaceScan?.receipt" size="small" text type="primary" @click="copyText(String(spaceScan.receipt))">
                                                {{ t('plugin.rolling-memory.system.copyBtn') }}
                                            </el-button>
                                        </div>
                                    </div>
                                </div>
                            </el-collapse-item>
                        </el-collapse>
                    </template>
                </section>

                <!-- 回收与自愈的大金库预演耗时可达数分钟，保留在「记忆总览」维护卡集中执行。 -->
                <p class="sys-hint sys-cross-link">
                    <el-button size="small" text type="primary" data-testid="btn-open-overview-maintenance" @click="goOverviewMaintenance">
                        {{ t('plugin.rolling-memory.system.openOverviewBtn') }}
                    </el-button>
                </p>
            </div>

            <aside class="sys-col-side">
                <!-- 缓存 -->
                <section class="sys-card" data-testid="card-system-cache">
                    <header class="sys-card-head">
                        <div class="sys-card-head-main">
                            <span class="sys-card-title">{{ t('plugin.rolling-memory.system.cacheTitle') }}</span>
                            <span class="sys-card-sub">{{ t('plugin.rolling-memory.system.cacheSub') }}</span>
                        </div>
                    </header>
                    <div class="sys-side-meta">
                        <span>{{ t('plugin.rolling-memory.system.lastClearedLbl') }}</span>
                        <span class="sys-mono">{{ lastClearedText || t('plugin.rolling-memory.system.neverCleared') }}</span>
                    </div>
                    <el-button size="small" :loading="cacheBusy" data-testid="btn-cache-clear" @click="clearCache">
                        {{ t('plugin.rolling-memory.system.clearBtn') }}
                    </el-button>
                    <p class="sys-hint">{{ t('plugin.rolling-memory.system.cacheHint') }}</p>
                </section>

                <!-- 系统信息 -->
                <section class="sys-card" data-testid="card-system-info">
                    <header class="sys-card-head">
                        <div class="sys-card-head-main">
                            <span class="sys-card-title">{{ t('plugin.rolling-memory.system.infoTitle') }}</span>
                        </div>
                    </header>
                    <div class="sys-info-list">
                        <div class="sys-info-row">
                            <span class="sys-info-lbl">{{ t('plugin.rolling-memory.system.modeLbl') }}</span>
                            <span class="sys-info-val">{{ t('plugin.rolling-memory.system.modeValue') }}</span>
                        </div>
                        <div class="sys-info-row">
                            <span class="sys-info-lbl">{{ t('plugin.rolling-memory.system.socketLbl') }}</span>
                            <span class="sys-info-val sys-mono sys-ellipsis" :title="daemon?.socket">{{ daemon?.socket || '—' }}</span>
                        </div>
                        <div class="sys-info-row">
                            <span class="sys-info-lbl">{{ t('plugin.rolling-memory.system.dbPathLbl') }}</span>
                            <span class="sys-info-val sys-mono sys-ellipsis" :title="daemon?.db">{{ daemon?.db || '—' }}</span>
                        </div>
                        <div class="sys-info-row">
                            <span class="sys-info-lbl">{{ t('plugin.rolling-memory.system.configPathLbl') }}</span>
                            <span class="sys-info-val sys-mono sys-ellipsis" :title="daemon?.config">{{ daemon?.config || '—' }}</span>
                        </div>
                    </div>
                </section>

                <!-- CLI 等价命令 -->
                <section class="sys-card" data-testid="card-system-cli">
                    <header class="sys-card-head">
                        <div class="sys-card-head-main">
                            <span class="sys-card-title">{{ t('plugin.rolling-memory.system.cliTitle') }}</span>
                            <span class="sys-card-sub">{{ t('plugin.rolling-memory.system.cliSub') }}</span>
                        </div>
                    </header>
                    <div class="sys-cli-list">
                        <div v-for="cmd in cliCommands" :key="cmd.key" class="sys-cli-row">
                            <div class="sys-cli-meta">
                                <span class="sys-cli-lbl">{{ cmd.label }}</span>
                                <code class="sys-cli-code">{{ cmd.command }}</code>
                            </div>
                            <el-button size="small" text type="primary" @click="copyText(cmd.command)">
                                {{ t('plugin.rolling-memory.system.copyBtn') }}
                            </el-button>
                        </div>
                    </div>
                </section>

                <!-- 相关入口：与「记忆控制台」「记忆总览」的分工见 linksSub -->
                <section class="sys-card" data-testid="card-system-links">
                    <header class="sys-card-head">
                        <div class="sys-card-head-main">
                            <span class="sys-card-title">{{ t('plugin.rolling-memory.system.linksTitle') }}</span>
                            <span class="sys-card-sub">{{ t('plugin.rolling-memory.system.linksSub') }}</span>
                        </div>
                    </header>
                    <div class="sys-link-list">
                        <el-button size="small" text type="primary" data-testid="btn-open-memory-console" @click="router.push('/admin/dashboard/artifacts')">
                            {{ t('plugin.rolling-memory.system.openConsoleBtn') }}
                        </el-button>
                        <el-button size="small" text type="primary" data-testid="btn-open-memory-overview" @click="router.push('/admin/plugin/rolling-memory/index')">
                            {{ t('plugin.rolling-memory.system.openOverviewIndexBtn') }}
                        </el-button>
                    </div>
                </section>
            </aside>
        </div>

        <systemUpdateDialog v-model="showSystemUpdateDialog" />
    </div>
</template>

<script setup lang="ts">
import { computed, onActivated, onBeforeUnmount, onDeactivated, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useAdminInfo } from '@/stores/adminInfo'
import { MonitorSocket } from '@/utils/monitorSocket'
import { copy } from '@/utils/common'
import request from '@/utils/request'
import systemUpdateDialog from './systemUpdateDialog.vue'
import { useUpdateStatus } from './useUpdateStatus'

defineOptions({
    name: 'plugin/rolling-memory/system',
})

const { t } = useI18n()
const router = useRouter()
const adminInfo = useAdminInfo()

const daemon = ref<any>(null)
const daemonBusy = ref('')
const daemonError = ref('')
const refreshing = ref(false)
const liveState = ref<'connecting' | 'connected' | 'reconnecting' | 'disabled'>('connecting')
const lastUpdatedAt = ref<number | null>(null)
const lastSampledAt = ref<number | null>(null)
const nowTick = ref(Date.now())
const fallbackDone = ref(false)

const memoryLimit = ref('auto')
const selectedPreset = ref('auto')
const customLimit = ref('')
const memorySaving = ref(false)
const restartPending = ref(false)

const cacheBusy = ref(false)
const lastCacheClearedAt = ref<number | null>(null)

const spaceBusy = ref('')
const spaceScan = ref<any>(null)
const lastApply = ref<any>(null)
const keepBackups = ref(0)

const showSystemUpdateDialog = ref(false)
const { state: updateState } = useUpdateStatus({
    onOpen: () => {
        showSystemUpdateDialog.value = true
    },
})

let systemSocket: MonitorSocket | null = null
let tickTimer: ReturnType<typeof setInterval> | null = null
let fallbackTimer: ReturnType<typeof setTimeout> | null = null

const daemonOnline = computed(() => Boolean(daemon.value?.online))
const configuredLimit = computed(() => String(daemon.value?.configured_memory_limit || ''))
const stale = computed(() => {
    if (!daemon.value || lastUpdatedAt.value == null) return false
    return liveState.value !== 'connected' || nowTick.value - lastUpdatedAt.value > 20000
})

const statusText = computed(() => {
    if (daemonError.value) return t('plugin.rolling-memory.system.statusError')
    return daemonOnline.value ? t('plugin.rolling-memory.system.running') : t('plugin.rolling-memory.system.stopped')
})
const statusTagType = computed(() => (daemonError.value ? 'danger' : daemonOnline.value ? 'success' : 'info'))
const statusDotClass = computed(() => (daemonError.value ? 'is-error' : daemonOnline.value ? 'is-online' : 'is-offline'))
const heroTone = computed(() => (daemonError.value ? 'is-error' : daemonOnline.value ? 'is-online' : 'is-offline'))

const liveLabel = computed(() => {
    switch (liveState.value) {
        case 'connected':
            return t('plugin.rolling-memory.system.liveConnected')
        case 'reconnecting':
            return t('plugin.rolling-memory.system.liveReconnecting')
        case 'disabled':
            return t('plugin.rolling-memory.system.liveDisabled')
        default:
            return t('plugin.rolling-memory.system.liveConnecting')
    }
})

const lastUpdatedText = computed(() => {
    if (lastUpdatedAt.value == null) return ''
    const date = new Date(lastUpdatedAt.value)
    return date.toLocaleTimeString(undefined, { hour12: false })
})

const liveUptime = computed(() => {
    const d = daemon.value
    if (!d) return 0
    if (typeof d.started_at === 'number' && d.started_at > 0) {
        return Math.max(0, Math.floor(nowTick.value / 1000 - d.started_at))
    }
    const base = Number(d.uptime_sec || 0)
    const elapsed = lastSampledAt.value != null ? Math.max(0, Math.floor((nowTick.value - lastSampledAt.value) / 1000)) : 0
    return base + elapsed
})

const startedAtText = computed(() => {
    const startedAt = daemon.value?.started_at
    if (typeof startedAt !== 'number' || startedAt <= 0) return ''
    return new Date(startedAt * 1000).toLocaleString()
})

const heapLimitPct = computed<number | null>(() => {
    const heap = Number(daemon.value?.heap_alloc_bytes || 0)
    const limit = Number(daemon.value?.mem_limit_bytes || 0)
    if (!daemon.value || limit <= 0 || heap <= 0) return null
    // 软上限允许短时超限（冷建/回填）：面板如实显示真实占比，进度条自身再钳位到 100%。
    return Math.round((heap / limit) * 1000) / 10
})
const heapProgressPct = computed(() => Math.min(100, heapLimitPct.value ?? 0))
const heapProgressColor = computed(() => {
    const pct = heapLimitPct.value ?? 0
    if (pct >= 90) return 'var(--el-color-danger)'
    if (pct >= 70) return 'var(--el-color-warning)'
    return 'var(--el-color-primary)'
})
const heapToneClass = computed(() => {
    const pct = heapLimitPct.value
    if (pct == null) return ''
    return pct >= 90 ? 'is-danger' : pct >= 70 ? 'is-warning' : ''
})

const ramPct = computed<number | null>(() => {
    const sys = Number(daemon.value?.heap_sys_bytes || 0)
    const total = Number(daemon.value?.total_ram_bytes || 0)
    if (!daemon.value || total <= 0 || sys <= 0) return null
    return Math.min(100, Math.round((sys / total) * 1000) / 10)
})
const ramText = computed(() => {
    const total = Number(daemon.value?.total_ram_bytes || 0)
    return total > 0 ? formatBytes(total) : ''
})

const effectiveLimitText = computed(() => {
    if (!daemonOnline.value) return '—'
    const source = String(daemon.value?.mem_limit_source || '')
    if (source === 'off') return t('plugin.rolling-memory.system.memoryOffOption')
    const bytes = Number(daemon.value?.mem_limit_bytes || 0)
    return bytes > 0 ? formatBytes(bytes) : '—'
})
const limitSourceText = computed(() => {
    switch (String(daemon.value?.mem_limit_source || '')) {
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
const limitSourceTagType = computed(() => {
    const source = String(daemon.value?.mem_limit_source || '')
    if (source === 'unavailable') return 'danger'
    if (source === 'off') return 'info'
    return 'success'
})
const memoryDirty = computed(() => memoryLimit.value !== configuredLimit.value)
const envOverrideActive = computed(() => String(daemon.value?.mem_limit_source || '') === 'env')
const envOverrideValue = computed(() => formatBytes(daemon.value?.mem_limit_bytes))

const spaceTotal = computed(() => Number(spaceScan.value?.reclaimable?.total_bytes || 0))
const spaceBackups = computed(() => Number(spaceScan.value?.reclaimable?.backups_bytes || 0))
const spaceCaches = computed(() => Number(spaceScan.value?.reclaimable?.caches_bytes || 0))
const spaceCompact = computed(() => Number(spaceScan.value?.reclaimable?.compact_bytes || 0))
const vaultBytes = computed(() => {
    const vault = spaceScan.value?.vault || {}
    return Number(vault.db_bytes || 0) + Number(vault.wal_bytes || 0) + Number(vault.sidecar_bytes || 0)
})
const logsBytes = computed(() => Number(spaceScan.value?.logs?.total_bytes || 0))
const spaceCleanActionable = computed(() => spaceScan.value !== null && spaceBackups.value + spaceCaches.value > 0)
const spaceCompactActionable = computed(() => spaceScan.value !== null && spaceCompact.value > 0)
const spaceStack = computed(() => {
    const segments = [
        { key: 'backups', label: t('plugin.rolling-memory.system.backupsLbl'), bytes: spaceBackups.value },
        { key: 'caches', label: t('plugin.rolling-memory.system.cachesLbl'), bytes: spaceCaches.value },
        { key: 'compact', label: t('plugin.rolling-memory.system.compactLbl'), bytes: spaceCompact.value },
    ]
    return segments.filter((segment) => segment.bytes > 0)
})

const lastClearedText = computed(() =>
    lastCacheClearedAt.value ? new Date(lastCacheClearedAt.value).toLocaleString() : ''
)

const cliCommands = computed(() => [
    { key: 'status', label: t('plugin.rolling-memory.system.cliStatusLbl'), command: 'rmem daemon status --json' },
    { key: 'limit', label: t('plugin.rolling-memory.system.cliLimitLbl'), command: 'rmem config set daemon.memory_limit 2GiB' },
    { key: 'maintain', label: t('plugin.rolling-memory.system.cliMaintainLbl'), command: 'rmem maintain status --json' },
])

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
    const days = Math.floor(seconds / 86400)
    const hours = Math.floor((seconds % 86400) / 3600)
    const minutes = Math.floor((seconds % 3600) / 60)
    if (days > 0) return `${days}d ${hours}h`
    if (hours > 0) return `${hours}h ${minutes}m`
    if (minutes > 0) return `${minutes}m ${seconds % 60}s`
    return `${seconds}s`
}

function spaceItems(section: string): number {
    const list = spaceScan.value?.[section]?.items
    return Array.isArray(list) ? list.length : 0
}
function spaceSectionBytes(section: string): number {
    return Number(spaceScan.value?.[section]?.total_bytes || 0)
}

function applyDaemonPayload(payload: any) {
    if (!payload || typeof payload !== 'object') return
    if (payload.sampled_at_ms) {
        lastSampledAt.value = Number(payload.sampled_at_ms)
    }
    if (payload.sampled_at) {
        lastSampledAt.value = Number(payload.sampled_at) * 1000
    }
    lastUpdatedAt.value = Date.now()
    if (payload.ok === false) {
        daemonError.value = String(payload.health_error || '')
        return
    }
    daemon.value = payload
    daemonError.value = String(payload.health_error || '')
    syncMemoryPreset(String(payload.configured_memory_limit || 'auto'))
}

function syncMemoryPreset(value: string) {
    const presets = ['auto', 'off', '512MiB', '1GiB', '2GiB', '4GiB']
    if (presets.includes(value)) {
        selectedPreset.value = value
        memoryLimit.value = value
        return
    }
    selectedPreset.value = 'custom'
    customLimit.value = value
    memoryLimit.value = value
}

function onPresetChange(value: string | number | boolean | undefined) {
    if (value === 'custom') {
        memoryLimit.value = customLimit.value
        return
    }
    memoryLimit.value = String(value ?? '')
}
function onCustomLimitInput(value: string) {
    memoryLimit.value = value
}

const startSystemStream = () => {
    stopSystemStream()
    const token = adminInfo.token
    if (!token) return
    if (typeof document !== 'undefined' && document.visibilityState === 'hidden') return
    systemSocket = new MonitorSocket({
        token,
        room: 'rm/system',
        onMessage: (frame: any) => {
            if (!frame?.event || frame.event !== 'system' || frame.data == null) return
            applyDaemonPayload(frame.data)
        },
        onStatus: (status) => {
            liveState.value = status
            if (status === 'disabled') {
                void fetchDaemonFallback()
            }
        },
    })
    systemSocket.start()
}

const stopSystemStream = () => {
    if (systemSocket) {
        systemSocket.stop()
        systemSocket = null
    }
}

const startTick = () => {
    stopTick()
    tickTimer = setInterval(() => {
        nowTick.value = Date.now()
    }, 1000)
}
const stopTick = () => {
    if (tickTimer) {
        clearInterval(tickTimer)
        tickTimer = null
    }
}

// 单次 REST 兜底：仅当实时通道不可用或首帧超时执行一次，不做任何定时轮询。
const fetchDaemonFallback = async () => {
    if (fallbackDone.value) return
    fallbackDone.value = true
    await loadDaemonOnce()
}

const loadDaemonOnce = async (manual = false) => {
    if (manual) refreshing.value = true
    try {
        const res = await request<any>({
            url: '/admin/plugin/rolling-memory/system/daemon',
            method: 'POST',
            data: { action: 'status' },
            timeout: 0,
        })
        const report = res.data?.data?.report
        if (report) applyDaemonPayload({ ...report, sampled_at_ms: Date.now() })
    } catch {
        if (manual && daemon.value) {
            ElMessage.warning(t('plugin.rolling-memory.system.refreshFailedToast'))
        }
    } finally {
        if (manual) refreshing.value = false
    }
}

const refreshNow = async () => {
    if (refreshing.value) return
    await loadDaemonOnce(true)
}

const confirmAction = async (message: string) => {
    try {
        await ElMessageBox.confirm(message, t('plugin.rolling-memory.system.confirmTitle'), {
            type: 'warning',
            confirmButtonText: t('plugin.rolling-memory.system.confirmOk'),
            cancelButtonText: t('plugin.rolling-memory.system.confirmCancel'),
        })
        return true
    } catch {
        return false
    }
}

const runDaemon = async (action: 'start' | 'stop' | 'restart') => {
    if (daemonBusy.value) return
    if (action === 'stop') {
        if (!(await confirmAction(t('plugin.rolling-memory.system.stopConfirm')))) return
    } else if (action === 'restart') {
        if (!(await confirmAction(t('plugin.rolling-memory.system.restartConfirm')))) return
    }
    daemonBusy.value = action
    try {
        const res = await request<any>({
            url: '/admin/plugin/rolling-memory/system/daemon',
            method: 'POST',
            data: { action },
            timeout: 0,
        })
        const report = res.data?.data?.report
        if (report) applyDaemonPayload({ ...report, sampled_at_ms: Date.now() })
        if (action === 'restart') restartPending.value = false
        ElMessage.success(t('plugin.rolling-memory.system.actionDoneToast'))
    } catch {
        // 错误提示由 request 统一弹出；保留上一次读数。
    } finally {
        daemonBusy.value = ''
    }
}

const saveMemoryLimit = async () => {
    if (memorySaving.value || !memoryLimit.value.trim()) return
    memorySaving.value = true
    try {
        const res = await request<any>({
            url: '/admin/plugin/rolling-memory/system/memory-limit',
            method: 'POST',
            data: { value: memoryLimit.value.trim() },
            timeout: 0,
        })
        const saved = String(res.data?.data?.memory_limit || memoryLimit.value)
        syncMemoryPreset(saved)
        daemon.value = { ...(daemon.value || {}), configured_memory_limit: saved }
        ElMessage.success(t('plugin.rolling-memory.system.savedToast'))
        if (res.data?.data?.restart_required) restartPending.value = true
    } catch {
        // 错误提示由 request 统一弹出（含服务端校验文案）。
    } finally {
        memorySaving.value = false
    }
}

const applyMemoryRestart = async () => {
    await runDaemon('restart')
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
        lastCacheClearedAt.value = Date.now()
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
        data: { action, apply, confirm: apply, caches: action === 'clean', keep: keepBackups.value },
        timeout: 0,
    })
    return res.data?.data?.report || null
}

const handleScan = async () => {
    if (spaceBusy.value) return
    spaceBusy.value = 'status'
    try {
        const report = await requestSpace('status')
        if (report) spaceScan.value = report
    } catch {
        // 错误提示由 request 统一弹出；保留上一次扫描读数。
    } finally {
        spaceBusy.value = ''
    }
}

const handleApply = async (action: 'clean' | 'compact') => {
    if (spaceBusy.value) return
    const key = action === 'clean' ? 'cleanConfirm' : 'compactConfirm'
    if (!(await confirmAction(t(`plugin.rolling-memory.system.${key}`)))) return
    spaceBusy.value = action
    try {
        const report = await requestSpace(action, true)
        if (report) {
            lastApply.value = report
            ElMessage.success(t('plugin.rolling-memory.system.doneToast'))
            // 应用报告不含 reclaimable：立即重扫一次，让读数与按钮状态回到真实空间面。
            const scan = await requestSpace('status')
            if (scan) spaceScan.value = scan
        }
    } catch {
        // 错误提示由 request 统一弹出。
    } finally {
        spaceBusy.value = ''
    }
}

const copyText = async (value: string) => {
    if (await copy(value)) {
        ElMessage.success(t('plugin.rolling-memory.system.copiedToast'))
    }
}

const goOverviewMaintenance = () => {
    void router.push('/admin/plugin/rolling-memory/index')
}

onMounted(() => {
    startSystemStream()
    startTick()
    fallbackTimer = setTimeout(() => {
        if (!daemon.value) void fetchDaemonFallback()
    }, 2500)
})

let deactivated = false
onActivated(() => {
    if (!deactivated) return
    deactivated = false
    startSystemStream()
    startTick()
})
onDeactivated(() => {
    deactivated = true
    stopSystemStream()
    stopTick()
})
onBeforeUnmount(() => {
    stopSystemStream()
    stopTick()
    if (fallbackTimer) {
        clearTimeout(fallbackTimer)
        fallbackTimer = null
    }
})
</script>

<style scoped>
.system-page {
    display: flex;
    flex-direction: column;
    gap: 14px;
}

.rm-update-dot {
    display: inline-block;
    width: 7px;
    height: 7px;
    margin-right: 6px;
    vertical-align: middle;
    border-radius: 50%;
    background: var(--el-color-danger);
    box-shadow: 0 0 0 2px rgba(245, 108, 108, 0.18);
}

/* ---------------- Hero ---------------- */
.sys-hero {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
    padding: 16px 18px;
    border: 1px solid var(--el-border-color-lighter);
    border-radius: 10px;
    background: var(--el-bg-color-overlay);
    position: relative;
    overflow: hidden;
}
.sys-hero::before {
    content: '';
    position: absolute;
    inset: 0 auto 0 0;
    width: 3px;
    background: var(--el-color-info-light-5);
}
.sys-hero.is-online::before {
    background: var(--el-color-success);
}
.sys-hero.is-error::before {
    background: var(--el-color-danger);
}
.sys-hero-left {
    display: flex;
    align-items: flex-start;
    gap: 14px;
    min-width: 0;
}
.sys-hero-badge {
    width: 40px;
    height: 40px;
    border-radius: 10px;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--el-color-primary);
    background: var(--el-color-primary-light-9);
    flex: none;
}
.sys-hero-titles {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
}
.sys-hero-title-row {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
}
.sys-hero-title {
    font-size: 20px;
    font-weight: 600;
    color: var(--el-text-color-primary);
}
.sys-hero-sub {
    font-size: 12px;
    color: var(--el-text-color-secondary);
}
.sys-mode-pill {
    font-family: var(--el-font-family-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
}
.sys-hero-right {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    justify-content: flex-end;
}
.sys-live {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: var(--el-text-color-secondary);
}
.sys-live-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--el-color-info);
    flex: none;
}
.sys-live.is-connected .sys-live-dot {
    background: var(--el-color-success);
    box-shadow: 0 0 0 0 var(--el-color-success-light-5);
    animation: sys-pulse 2s ease-out infinite;
}
.sys-live.is-reconnecting .sys-live-dot,
.sys-live.is-connecting .sys-live-dot {
    background: var(--el-color-warning);
}
.sys-live.is-disabled .sys-live-dot {
    background: var(--el-color-danger);
}
.sys-live-time {
    color: var(--el-text-color-placeholder);
    font-variant-numeric: tabular-nums;
}
@keyframes sys-pulse {
    0% {
        box-shadow: 0 0 0 0 rgba(103, 194, 58, 0.45);
    }
    70% {
        box-shadow: 0 0 0 6px rgba(103, 194, 58, 0);
    }
    100% {
        box-shadow: 0 0 0 0 rgba(103, 194, 58, 0);
    }
}
.sys-status-dot {
    display: inline-block;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    margin-right: 5px;
    background: currentColor;
}
.sys-status-dot.is-online {
    background: #fff;
}
.sys-status-dot.is-offline {
    background: #fff;
    opacity: 0.7;
}

/* ---------------- KPI ---------------- */
.sys-kpis {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 12px;
}
.sys-kpi {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 14px 16px;
    border: 1px solid var(--el-border-color-lighter);
    border-radius: 10px;
    background: var(--el-bg-color-overlay);
    transition:
        transform 0.18s ease,
        border-color 0.18s ease;
    min-width: 0;
}
.sys-kpi:hover {
    transform: translateY(-1px);
    border-color: var(--el-color-primary-light-7);
}
.sys-kpi.is-warning {
    border-color: var(--el-color-warning-light-7);
}
.sys-kpi.is-danger {
    border-color: var(--el-color-danger-light-7);
}
.sys-kpi-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    font-size: 12px;
    color: var(--el-text-color-secondary);
}
.sys-kpi-num {
    font-size: 26px;
    font-weight: 700;
    line-height: 1.1;
    color: var(--el-text-color-primary);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}
.sys-kpi-sub {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
    font-size: 11px;
    color: var(--el-text-color-secondary);
    min-height: 18px;
}
.sys-kpi-sub-text {
    color: var(--el-text-color-placeholder);
}
.sys-kpi-progress {
    margin: 2px 0;
}
.sys-kpi-skeleton {
    padding-top: 6px;
}
.sys-sep {
    color: var(--el-text-color-placeholder);
}

/* ---------------- Layout ---------------- */
.sys-main {
    display: grid;
    grid-template-columns: minmax(0, 2fr) minmax(0, 1fr);
    gap: 14px;
    align-items: start;
}
.sys-col-main,
.sys-col-side {
    display: flex;
    flex-direction: column;
    gap: 14px;
    min-width: 0;
}
.sys-card {
    padding: 16px 18px;
    border: 1px solid var(--el-border-color-lighter);
    border-radius: 10px;
    background: var(--el-bg-color-overlay);
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
}
.sys-card-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
}
.sys-card-head-main {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
}
.sys-card-title {
    font-size: 14px;
    font-weight: 600;
    color: var(--el-text-color-primary);
}
.sys-card-sub {
    font-size: 12px;
    color: var(--el-text-color-secondary);
}
.sys-card-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
}
.sys-hint {
    margin: 0;
    font-size: 12px;
    color: var(--el-text-color-secondary);
}
.sys-cross-link {
    display: flex;
    justify-content: flex-end;
}
.sys-mono {
    font-family: var(--el-font-family-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
    font-size: 12px;
}
.sys-ellipsis {
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    display: inline-block;
    vertical-align: bottom;
}
.sys-alert {
    margin-top: 2px;
}
.sys-alert-body {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    flex-wrap: wrap;
}
.sys-alert-text {
    font-size: 12px;
    word-break: break-all;
}

/* ---------------- Empty states ---------------- */
.sys-empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 26px 12px;
    color: var(--el-text-color-secondary);
    border: 1px dashed var(--el-border-color-light);
    border-radius: 8px;
}
.sys-empty-inline {
    flex-direction: row;
    padding: 16px 12px;
    gap: 10px;
}
.sys-empty-title {
    font-size: 14px;
    font-weight: 600;
    color: var(--el-text-color-primary);
}
.sys-empty-desc {
    font-size: 12px;
    text-align: center;
}

/* ---------------- Descriptions ---------------- */
.sys-desc :deep(.el-descriptions__label) {
    width: 108px;
    white-space: nowrap;
}

/* ---------------- Memory ---------------- */
.sys-preset-row {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
}
.sys-preset-label,
.sys-keep-label {
    font-size: 12px;
    color: var(--el-text-color-secondary);
    white-space: nowrap;
}
.sys-custom-limit {
    width: 150px;
}
.sys-keep-select {
    width: 120px;
}
.sys-compare {
    display: flex;
    align-items: center;
    gap: 14px;
    flex-wrap: wrap;
    padding: 10px 14px;
    border-radius: 8px;
    background: var(--el-fill-color-light);
}
.sys-compare-item {
    display: flex;
    flex-direction: column;
    gap: 2px;
}
.sys-compare-lbl {
    font-size: 11px;
    color: var(--el-text-color-secondary);
}
.sys-compare-val {
    font-size: 14px;
    font-weight: 600;
    color: var(--el-text-color-primary);
}
.sys-compare-arrow {
    color: var(--el-text-color-placeholder);
}

/* ---------------- Metrics & disk ---------------- */
.sys-metrics {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
    gap: 10px;
}
.sys-metric {
    display: flex;
    flex-direction: column;
    gap: 3px;
    padding: 10px 12px;
    border-radius: 8px;
    background: var(--el-fill-color-lighter);
    min-width: 0;
}
.sys-metric.is-accent {
    background: var(--el-color-primary-light-9);
}
.sys-metric-num {
    font-size: 18px;
    font-weight: 600;
    color: var(--el-text-color-primary);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}
.sys-metric-lbl {
    font-size: 11px;
    color: var(--el-text-color-secondary);
}
.sys-stack {
    display: flex;
    gap: 2px;
    height: 10px;
    border-radius: 999px;
    overflow: hidden;
    background: var(--el-fill-color-lighter);
}
.sys-stack-seg {
    height: 100%;
    min-width: 2px;
}
.sys-stack-seg.is-backups,
.sys-legend-dot.is-backups {
    background: var(--el-color-warning);
}
.sys-stack-seg.is-caches,
.sys-legend-dot.is-caches {
    background: var(--el-color-primary);
}
.sys-stack-seg.is-compact,
.sys-legend-dot.is-compact {
    background: var(--el-color-success);
}
.sys-stack-legend {
    display: flex;
    align-items: center;
    gap: 16px;
    flex-wrap: wrap;
    font-size: 11px;
    color: var(--el-text-color-secondary);
}
.sys-legend-item {
    display: inline-flex;
    align-items: center;
    gap: 6px;
}
.sys-legend-dot {
    width: 8px;
    height: 8px;
    border-radius: 2px;
    display: inline-block;
}
.sys-apply-result {
    display: flex;
    align-items: center;
    gap: 14px;
    flex-wrap: wrap;
    font-size: 12px;
    color: var(--el-text-color-secondary);
}
.sys-apply-result strong {
    color: var(--el-color-success);
    font-variant-numeric: tabular-nums;
}
.sys-receipt {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: var(--el-text-color-secondary);
    flex-wrap: wrap;
}
.sys-receipt code {
    font-family: var(--el-font-family-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
    font-size: 11px;
    word-break: break-all;
}
.sys-collapse {
    border-top: 1px dashed var(--el-border-color-lighter);
    border-bottom: none;
}
.sys-detail-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
    gap: 12px;
}
.sys-detail-block {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 10px 12px;
    border-radius: 8px;
    background: var(--el-fill-color-lighter);
}
.sys-detail-title {
    font-size: 12px;
    font-weight: 600;
    color: var(--el-text-color-primary);
}
.sys-detail-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    font-size: 12px;
    color: var(--el-text-color-secondary);
    min-width: 0;
}

/* ---------------- Side column ---------------- */
.sys-side-meta {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    font-size: 12px;
    color: var(--el-text-color-secondary);
    padding: 10px 12px;
    border-radius: 8px;
    background: var(--el-fill-color-lighter);
}
.sys-info-list,
.sys-cli-list {
    display: flex;
    flex-direction: column;
    gap: 10px;
}
.sys-info-row {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    font-size: 12px;
    min-width: 0;
}
.sys-info-lbl {
    color: var(--el-text-color-secondary);
    white-space: nowrap;
}
.sys-info-val {
    color: var(--el-text-color-primary);
    text-align: right;
    min-width: 0;
}
.sys-cli-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 8px 10px;
    border-radius: 8px;
    background: var(--el-fill-color-lighter);
}
.sys-cli-meta {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
}
.sys-cli-lbl {
    font-size: 11px;
    color: var(--el-text-color-secondary);
}
.sys-cli-code {
    font-family: var(--el-font-family-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
    font-size: 12px;
    color: var(--el-text-color-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}
.sys-link-list {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
}

/* ---------------- Responsive ---------------- */
@media (max-width: 1200px) {
    .sys-main {
        grid-template-columns: minmax(0, 1fr);
    }
}
@media (max-width: 900px) {
    .sys-kpis {
        grid-template-columns: repeat(2, minmax(0, 1fr));
    }
}
@media (max-width: 640px) {
    .sys-hero {
        flex-direction: column;
    }
    .sys-hero-right {
        justify-content: flex-start;
    }
    .sys-kpis {
        grid-template-columns: minmax(0, 1fr);
    }
}
</style>
