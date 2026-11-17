<!-- plugin: rolling-memory v0.0.58 -->
<template>
    <div class="default-main plugin-rolling-memory">
        <!-- 顶部提示条（参照 plugins/ai/web/views/channel/index.vue 标准） -->
        <el-alert
            class="rm-alert"
            type="info"
            :closable="false"
            show-icon
            :title="t('plugin.rolling-memory.background.pageTitle')"
            :description="`${t('plugin.rolling-memory.background.pageSub')} · ${t('plugin.rolling-memory.background.nativeHint')}`"
        />

        <!-- 4 维统计指标条 -->
        <div class="rm-stats-bar">
            <div class="stat-item primary">
                <span class="stat-label">{{ t('plugin.rolling-memory.background.dirsLabel') }}</span>
                <span class="stat-value">{{ dirs.length }}</span>
            </div>
            <div class="stat-item info">
                <span class="stat-label">{{ t('plugin.rolling-memory.background.filesLabel') }}</span>
                <span class="stat-value">{{ stats.background_file_count || files.length }}</span>
            </div>
            <div class="stat-item success">
                <span class="stat-label">{{ t('plugin.rolling-memory.background.chunksLabel') }}</span>
                <span class="stat-value">{{ stats.background_count || 0 }}</span>
            </div>
            <div class="stat-item" :class="!alignmentKnown ? 'info' : bgAlign.aligned ? 'success' : 'warning'">
                <span class="stat-label">{{
                    isLocalMode
                        ? t('plugin.rolling-memory.overview.localVaultLbl')
                        : `${t('plugin.rolling-memory.overview.localVaultLbl')} ⇄ ${t('plugin.rolling-memory.overview.remoteCloudLbl')}`
                }}</span>
                <span v-if="isLocalMode" class="stat-value">{{ bgAlign.local_vault_count ?? stats.background_count ?? 0 }}</span>
                <span v-else-if="!alignmentKnown" class="stat-value">{{ t('plugin.rolling-memory.overview.unknownStatus') }}</span>
                <span v-else class="stat-value"
                    >{{ bgAlign.local_vault_count }} / {{ bgAlign.remote_cloud_count }} ({{ bgAlign.alignment_pct }}%)</span
                >
            </div>
        </div>

        <!-- 挂载目录 ⇄ 目录内文件 双栏联动面板 -->
        <el-row :gutter="12" class="mb-10">
            <el-col :xs="24" :md="11">
                <div class="sub-table-box">
                    <div class="sub-table-header">
                        <div class="sub-table-title">
                            <Icon name="lucide-folder-tree" :size="15" />
                            <span>{{ t('plugin.rolling-memory.background.mountTitle') }}</span>
                        </div>
                        <div class="sub-table-actions">
                            <el-button
                                v-blur
                                type="primary"
                                size="small"
                                :loading="pickingDir || mountingDir"
                                :disabled="isBgTaskBusy && !pickingDir && !mountingDir"
                                data-testid="btn-add-bg-dir"
                                @click="handleAddDir"
                            >
                                <Icon v-if="!pickingDir && !mountingDir" name="lucide-folder-open" :size="13" class="btn-icon" />
                                {{
                                    pickingDir
                                        ? t('plugin.rolling-memory.background.waitingForPicker')
                                        : mountingDir
                                          ? `${t('plugin.rolling-memory.background.mounting')} (${mountDirPct}%)`
                                          : t('plugin.rolling-memory.background.selectAndMount')
                                }}
                            </el-button>
                            <el-button
                                v-blur
                                size="small"
                                :disabled="isBgTaskBusy"
                                data-testid="btn-manual-bg-dir"
                                @click="openManualMountDialog()"
                            >
                                <Icon name="lucide-pen-line" :size="13" class="btn-icon" />
                                {{ t('plugin.rolling-memory.background.manualBtn') }}
                            </el-button>
                        </div>
                    </div>
                    <!-- 添加挂载时的文档类型过滤（多选，默认选中 .md） -->
                    <div class="ext-filter-bar" data-testid="bg-ext-filter-bar">
                        <div class="ext-filter-head">
                            <span class="ext-filter-label">{{ t('plugin.rolling-memory.background.extFilterLabel') }}</span>
                            <div class="ext-filter-presets">
                                <el-button
                                    link
                                    size="small"
                                    :type="isMdOnlySelected ? 'primary' : 'default'"
                                    data-testid="btn-ext-preset-md"
                                    @click="applyExtPreset('md')"
                                >
                                    {{ t('plugin.rolling-memory.background.extPresetMdOnly') }}
                                </el-button>
                                <el-button
                                    link
                                    size="small"
                                    :type="isCommonDocsSelected ? 'primary' : 'default'"
                                    data-testid="btn-ext-preset-docs"
                                    @click="applyExtPreset('docs')"
                                >
                                    {{ t('plugin.rolling-memory.background.extPresetDocs') }}
                                </el-button>
                                <el-button
                                    link
                                    size="small"
                                    :type="isAllExtsSelected ? 'primary' : 'default'"
                                    data-testid="btn-ext-preset-all"
                                    @click="applyExtPreset('all')"
                                >
                                    {{ t('plugin.rolling-memory.background.extPresetAll') }}
                                </el-button>
                            </div>
                        </div>
                        <el-checkbox-group
                            v-model="selectedExtTokens"
                            size="small"
                            class="ext-checkbox-group"
                            :disabled="isBgTaskBusy"
                            @change="onExtTokensChange"
                        >
                            <el-checkbox
                                v-for="opt in extFilterOptions"
                                :key="opt.value"
                                :value="opt.value"
                                :data-testid="`chk-ext-${opt.value.replace(/^\./, '')}`"
                            >
                                {{ opt.label }}
                            </el-checkbox>
                        </el-checkbox-group>
                    </div>
                    <el-table
                        ref="dirsTableRef"
                        :data="dirs"
                        row-key="dir_id"
                        size="small"
                        border
                        stripe
                        class="ag-data-table"
                        header-cell-class-name="table-header-cell"
                        v-loading="dirsTableLoading"
                        :element-loading-text="dirsLoadingText"
                        :empty-text="t('plugin.rolling-memory.background.empty')"
                        max-height="220"
                        highlight-current-row
                        @row-click="onDirRowClick"
                    >
                        <el-table-column
                            prop="root_path"
                            :label="t('plugin.rolling-memory.background.colPath')"
                            min-width="170"
                            show-overflow-tooltip
                        />
                        <el-table-column :label="t('plugin.rolling-memory.background.colExtFilter')" width="108" align="center">
                            <template #default="{ row }">
                                <el-tag
                                    size="small"
                                    effect="plain"
                                    :type="row.include_exts?.length ? 'primary' : 'info'"
                                    class="dir-ext-tag"
                                    :title="formatDirExtsTooltip(row.include_exts)"
                                    @click.stop="openEditDirFilterDialog(row)"
                                >
                                    {{ formatDirExtsLabel(row.include_exts) }}
                                </el-tag>
                            </template>
                        </el-table-column>
                        <el-table-column prop="file_count" :label="t('plugin.rolling-memory.background.colFileCount')" width="66" align="center" />
                        <el-table-column prop="chunk_count" :label="t('plugin.rolling-memory.background.colChunkCount')" width="66" align="center" />
                        <el-table-column :label="t('plugin.rolling-memory.background.colAction')" width="152" align="center">
                            <template #default="{ row }">
                                <el-button
                                    link
                                    :type="selectedDir?.dir_id === row.dir_id ? 'success' : 'primary'"
                                    size="small"
                                    data-testid="btn-browse-bg-files"
                                    @click.stop="onDirRowClick(row)"
                                >
                                    {{
                                        selectedDir?.dir_id === row.dir_id
                                            ? t('plugin.rolling-memory.background.focusedBtn')
                                            : t('plugin.rolling-memory.background.browseBtn')
                                    }}
                                </el-button>
                                <el-button
                                    link
                                    type="primary"
                                    size="small"
                                    :disabled="isBgTaskBusy"
                                    data-testid="btn-edit-bg-dir-filter"
                                    @click.stop="openEditDirFilterDialog(row)"
                                >
                                    {{ t('plugin.rolling-memory.background.editFilterBtn') }}
                                </el-button>
                                <el-button
                                    link
                                    type="danger"
                                    size="small"
                                    :loading="removingDirId === row.dir_id"
                                    :disabled="isBgTaskBusy && removingDirId !== row.dir_id"
                                    data-testid="btn-remove-bg-dir"
                                    @click.stop="handleRemoveDir(row)"
                                >
                                    {{
                                        removingDirId === row.dir_id
                                            ? `${t('plugin.rolling-memory.background.removeBtn')} (${removeDirPct}%)`
                                            : t('plugin.rolling-memory.background.removeBtn')
                                    }}
                                </el-button>
                            </template>
                        </el-table-column>
                    </el-table>
                </div>
            </el-col>

            <el-col :xs="24" :md="13">
                <div class="sub-table-box">
                    <div class="sub-table-header">
                        <div class="sub-table-title">
                            <Icon name="lucide-file-stack" :size="15" />
                            <span>{{ t('plugin.rolling-memory.background.filesTitle') }}</span>
                            <el-tag
                                v-if="selectedDir"
                                size="small"
                                type="primary"
                                closable
                                data-testid="btn-clear-dir-filter"
                                @close="selectDir(null)"
                            >
                                {{ selectedDir.root_path }}
                            </el-tag>
                            <el-tag
                                v-if="selectedRelPath"
                                size="small"
                                type="success"
                                closable
                                data-testid="btn-clear-file-filter-top"
                                @close="clearSelectedFile()"
                            >
                                {{ t('plugin.rolling-memory.background.focusingFileTag') }}{{ selectedRelPath }}
                            </el-tag>
                        </div>
                        <div class="sub-table-actions">
                            <el-input
                                v-model="fileKeyword"
                                size="small"
                                clearable
                                :placeholder="t('plugin.rolling-memory.filters.searchPlaceholder')"
                                style="width: 170px"
                                @input="onFileKeywordInput"
                                @clear="loadFiles"
                                @keyup.enter="loadFiles"
                            />
                        </div>
                    </div>
                    <div class="sub-table-hint">
                        {{ t('plugin.rolling-memory.background.manualHint') }}
                    </div>
                    <el-table
                        ref="filesTableRef"
                        :data="pagedFiles"
                        row-key="abs_path"
                        size="small"
                        border
                        stripe
                        class="ag-data-table"
                        header-cell-class-name="table-header-cell"
                        v-loading="filesTableLoading"
                        :element-loading-text="filesLoadingText"
                        :empty-text="t('plugin.rolling-memory.background.filesEmpty')"
                        max-height="220"
                        highlight-current-row
                        @row-click="focusFileChunks"
                    >
                        <el-table-column
                            prop="rel_path"
                            :label="t('plugin.rolling-memory.background.colRelPath')"
                            min-width="200"
                            show-overflow-tooltip
                        />
                        <el-table-column prop="size_text" :label="t('plugin.rolling-memory.background.colSize')" width="86" align="center" />
                        <el-table-column prop="chunk_count" :label="t('plugin.rolling-memory.background.colChunkCount')" width="76" align="center" />
                        <el-table-column prop="updated_text" :label="t('plugin.rolling-memory.background.colUpdatedAt')" width="148" align="center" />
                        <el-table-column :label="t('plugin.rolling-memory.background.colAction')" width="96" align="center">
                            <template #default="{ row }">
                                <el-button
                                    link
                                    :type="selectedRelPath === row.rel_path ? 'success' : 'primary'"
                                    size="small"
                                    data-testid="btn-browse-bg-chunks"
                                    @click.stop="focusFileChunks(row)"
                                >
                                    {{
                                        selectedRelPath === row.rel_path
                                            ? t('plugin.rolling-memory.background.focusedBtn')
                                            : t('plugin.rolling-memory.background.chunksBtn')
                                    }}
                                </el-button>
                            </template>
                        </el-table-column>
                    </el-table>
                    <div v-if="files.length > filePageSize" class="files-pager">
                        <el-pagination
                            v-model:current-page="filePage"
                            :page-size="filePageSize"
                            :total="files.length"
                            layout="total, prev, pager, next"
                            size="small"
                        />
                    </div>
                </div>
            </el-col>
        </el-row>

        <!-- 背景分块/单元主表（100% 原生 BuildAdmin TableHeader + Table + ComSearch + 分页） -->
        <layerBrowser
            ref="browserRef"
            layer="background"
            :dir-id="selectedDirId"
            :rel-path="selectedRelPath"
            :title="t('plugin.rolling-memory.background.unitsTitle')"
            @loaded="onStatsLoaded"
            @deleted="onUnitsDeleted"
        >
            <template #toolbarAppend>
                <el-switch
                    v-model="envHealth.auto_sync_enabled"
                    :loading="togglingAutoSync"
                    :disabled="typeof envHealth.auto_sync_enabled !== 'boolean' || togglingAutoSync"
                    class="btns-ml-12"
                    :active-text="t('plugin.rolling-memory.overview.autoSyncModeLbl')"
                    :inactive-text="t('plugin.rolling-memory.overview.manualSyncModeLbl')"
                    @change="handleToggleAutoSync"
                />
                <el-button
                    v-blur
                    type="primary"
                    class="table-header-operate btns-ml-12"
                    :loading="pickingDir || mountingDir"
                    :disabled="isBgTaskBusy && !pickingDir && !mountingDir"
                    @click="handleAddDir"
                >
                    <Icon size="14" name="lucide-folder-plus" color="#ffffff" />
                    <span class="table-header-operate-text">
                        {{ t('plugin.rolling-memory.background.selectAndMount') }}
                        <template v-if="mountingDir"> ({{ mountDirPct }}%)</template>
                    </span>
                </el-button>
                <el-button
                    v-blur
                    type="success"
                    class="table-header-operate btns-ml-12"
                    :loading="syncing"
                    :disabled="isBgTaskBusy && !syncing"
                    data-testid="btn-sync-all"
                    @click="handleSync"
                >
                    <Icon size="14" name="lucide-refresh-cw" color="#ffffff" />
                    <span class="table-header-operate-text">
                        {{ t('plugin.rolling-memory.background.syncBtn') }}
                        <template v-if="syncing"> ({{ bgSyncPct }}%)</template>
                    </span>
                </el-button>
                <el-button
                    v-blur
                    type="warning"
                    plain
                    class="table-header-operate btns-ml-12"
                    :loading="reconciling"
                    :disabled="isBgTaskBusy && !reconciling"
                    data-testid="btn-reconcile-bg"
                    @click="handleReconcileBg"
                >
                    <Icon size="14" name="lucide-refresh-cw" />
                    <span class="table-header-operate-text">
                        {{ t('plugin.rolling-memory.overview.reconcileLayerBtn') }}
                        <template v-if="reconciling"> ({{ bgReconcilePct }}%)</template>
                        <template v-else-if="bgAlign.total_pending_sync > 0"> ({{ bgAlign.total_pending_sync }})</template>
                    </span>
                </el-button>
            </template>

            <template #searchPrepend>
                <el-tag v-if="selectedRelPath" size="small" type="primary" closable class="filter-tag" @close="clearSelectedFile()">
                    {{ t('plugin.rolling-memory.background.colRelPath') }}: {{ selectedRelPath }}
                </el-tag>
            </template>
        </layerBrowser>

        <!-- 手动挂载目录 / 修改已挂载目录文档类型过滤对话框 -->
        <el-dialog
            v-model="manualDialogVisible"
            :title="
                manualDialogMode === 'edit'
                    ? t('plugin.rolling-memory.background.editFilterTitle')
                    : t('plugin.rolling-memory.background.manualTitle')
            "
            width="min(560px, 94vw)"
            :close-on-click-modal="!mountingDir"
            :close-on-press-escape="!mountingDir"
            :show-close="!mountingDir"
            destroy-on-close
        >
            <div class="manual-mount-dialog-body">
                <p class="manual-dialog-desc">
                    {{
                        manualDialogMode === 'edit'
                            ? t('plugin.rolling-memory.background.editFilterPrompt')
                            : t('plugin.rolling-memory.background.manualPrompt')
                    }}
                </p>
                <el-input
                    v-model="manualDialogPath"
                    :disabled="manualDialogMode === 'edit' || mountingDir"
                    :placeholder="t('plugin.rolling-memory.background.manualPlaceholder')"
                    clearable
                    data-testid="input-manual-bg-path"
                    @keyup.enter="submitManualDialog"
                />
                <div class="ext-filter-bar dialog-ext-bar" data-testid="dialog-ext-filter-bar">
                    <div class="ext-filter-head">
                        <span class="ext-filter-label">{{ t('plugin.rolling-memory.background.extFilterLabel') }}</span>
                        <div class="ext-filter-presets">
                            <el-button link size="small" type="primary" :disabled="mountingDir" @click="applyDialogExtPreset('md')">
                                {{ t('plugin.rolling-memory.background.extPresetMdOnly') }}
                            </el-button>
                            <el-button link size="small" :disabled="mountingDir" @click="applyDialogExtPreset('docs')">
                                {{ t('plugin.rolling-memory.background.extPresetDocs') }}
                            </el-button>
                            <el-button link size="small" :disabled="mountingDir" @click="applyDialogExtPreset('all')">
                                {{ t('plugin.rolling-memory.background.extPresetAll') }}
                            </el-button>
                        </div>
                    </div>
                    <el-checkbox-group
                        v-model="manualDialogExtTokens"
                        size="small"
                        class="ext-checkbox-group"
                        :disabled="mountingDir"
                        @change="onDialogExtTokensChange"
                    >
                        <el-checkbox v-for="opt in extFilterOptions" :key="opt.value" :value="opt.value">
                            {{ opt.label }}
                        </el-checkbox>
                    </el-checkbox-group>
                </div>
            </div>
            <template #footer>
                <el-button :disabled="mountingDir" @click="manualDialogVisible = false">
                    {{ t('common.cancel') }}
                </el-button>
                <el-button
                    type="primary"
                    :loading="mountingDir"
                    :disabled="!manualDialogPath.trim()"
                    data-testid="btn-confirm-manual-mount"
                    @click="submitManualDialog"
                >
                    {{
                        mountingDir
                            ? `${t('plugin.rolling-memory.background.mounting')} (${mountDirPct}%)`
                            : manualDialogMode === 'edit'
                              ? t('plugin.rolling-memory.background.saveAndResyncBtn')
                              : t('plugin.rolling-memory.background.mountAndSync')
                    }}
                </el-button>
            </template>
        </el-dialog>
    </div>
</template>

<script setup lang="ts">
import { useAdminInfo } from '@/stores/adminInfo'
import { useLoadingProgressText } from '@/utils/loadingProgress'
import { MonitorSocket } from '@/utils/monitorSocket'
import request from '@/utils/request'
import { ElMessage, ElMessageBox } from 'element-plus'
import { computed, nextTick, onActivated, onBeforeUnmount, onDeactivated, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import layerBrowser from './layerBrowser.vue'

defineOptions({
    name: 'plugin/rolling-memory/background',
})

const { t } = useI18n()
const adminInfo = useAdminInfo()
let envSocket: MonitorSocket | null = null

const CODE_GROUP_EXTS = [
    '.go', '.py', '.ts', '.tsx', '.js', '.jsx', '.mjs', '.cjs', '.vue', '.svelte',
    '.java', '.kt', '.rs', '.c', '.cc', '.cpp', '.h', '.hpp', '.cs', '.swift',
    '.rb', '.php', '.lua', '.sql', '.sh', '.bash', '.zsh', '.dockerfile', '.makefile',
    '.proto', '.graphql', '.css', '.scss', '.html',
]
const COMMON_DOC_TOKENS = ['.md', '.txt', '.pdf', '.docx', '.xlsx', '.pptx']

const extFilterOptions = computed(() => [
    { value: '.md', label: '.md', exts: ['.md'] },
    { value: '.txt', label: '.txt', exts: ['.txt', '.rst', '.org'] },
    { value: '.pdf', label: '.pdf', exts: ['.pdf'] },
    { value: '.docx', label: '.docx', exts: ['.docx'] },
    { value: '.xlsx', label: '.xlsx', exts: ['.xlsx'] },
    { value: '.pptx', label: '.pptx', exts: ['.pptx'] },
    { value: '.json', label: '.json', exts: ['.json', '.jsonl'] },
    { value: '.csv', label: '.csv', exts: ['.csv', '.tsv'] },
    { value: '.yaml', label: '.yaml', exts: ['.yaml', '.toml', '.ini', '.xml'] },
    { value: 'code', label: t('plugin.rolling-memory.background.extCodeGroup'), exts: CODE_GROUP_EXTS },
])

// 默认选中 .md（避免挂载代码库或杂项目录时把大量机器生成 JSON/CSV/构建源码误扫入 L1）。
const selectedExtTokens = ref<string[]>(['.md'])

const allOptionTokens = computed(() => extFilterOptions.value.map((o) => o.value))
const isMdOnlySelected = computed(() => selectedExtTokens.value.length === 1 && selectedExtTokens.value[0] === '.md')
const isCommonDocsSelected = computed(
    () =>
        selectedExtTokens.value.length === COMMON_DOC_TOKENS.length &&
        COMMON_DOC_TOKENS.every((tok) => selectedExtTokens.value.includes(tok))
)
const isAllExtsSelected = computed(
    () =>
        selectedExtTokens.value.length === allOptionTokens.value.length &&
        allOptionTokens.value.every((tok) => selectedExtTokens.value.includes(tok))
)

const applyExtPreset = (preset: 'md' | 'docs' | 'all') => {
    if (preset === 'md') {
        selectedExtTokens.value = ['.md']
    } else if (preset === 'docs') {
        selectedExtTokens.value = [...COMMON_DOC_TOKENS]
    } else {
        selectedExtTokens.value = [...allOptionTokens.value]
    }
}

const onExtTokensChange = (val: any[]) => {
    if (!Array.isArray(val) || val.length === 0) {
        selectedExtTokens.value = ['.md']
        ElMessage.warning(t('plugin.rolling-memory.background.extEmptyWarn'))
    }
}

const expandTokensToExts = (tokens: string[]): string[] => {
    const clean = Array.isArray(tokens) && tokens.length > 0 ? tokens : ['.md']
    if (clean.length === allOptionTokens.value.length && allOptionTokens.value.every((tok) => clean.includes(tok))) {
        return ['*']
    }
    const out = new Set<string>()
    for (const opt of extFilterOptions.value) {
        if (clean.includes(opt.value)) {
            for (const ext of opt.exts) out.add(ext)
        }
    }
    return out.size > 0 ? Array.from(out) : ['.md']
}

const storedExtsToTokens = (includeExts?: string[]): string[] => {
    if (!Array.isArray(includeExts) || includeExts.length === 0) {
        return [...allOptionTokens.value]
    }
    const lowerSet = new Set(includeExts.map((s) => String(s || '').toLowerCase()))
    const tokens: string[] = []
    for (const opt of extFilterOptions.value) {
        if (opt.exts.some((e) => lowerSet.has(e))) {
            tokens.push(opt.value)
        }
    }
    return tokens.length > 0 ? tokens : ['.md']
}

const formatDirExtsLabel = (includeExts?: string[]): string => {
    if (!Array.isArray(includeExts) || includeExts.length === 0) {
        return t('plugin.rolling-memory.background.allFormatsTag')
    }
    const tokens = storedExtsToTokens(includeExts)
    if (tokens.length === allOptionTokens.value.length) {
        return t('plugin.rolling-memory.background.allFormatsTag')
    }
    const labels = tokens.map((tok) => (tok === 'code' ? t('plugin.rolling-memory.background.extCodeGroup') : tok))
    if (labels.length <= 2) return labels.join(' ')
    return `${labels.slice(0, 2).join(' ')} +${labels.length - 2}`
}

const formatDirExtsTooltip = (includeExts?: string[]): string => {
    if (!Array.isArray(includeExts) || includeExts.length === 0) {
        return t('plugin.rolling-memory.background.allFormatsTag')
    }
    return includeExts.join(', ')
}

const loadingDirs = ref(false)
const loadingFiles = ref(false)
const syncing = ref(false)
const reconciling = ref(false)
const pickingDir = ref(false)
const mountingDir = ref(false)
const removingDirId = ref<string>('')
const togglingAutoSync = ref(false)

const isBgTaskBusy = computed(
    () => pickingDir.value || mountingDir.value || Boolean(removingDirId.value) || syncing.value || reconciling.value
)
const dirsTableLoading = computed(
    () => loadingDirs.value || mountingDir.value || Boolean(removingDirId.value) || syncing.value || reconciling.value
)
const filesTableLoading = computed(
    () =>
        loadingFiles.value ||
        mountingDir.value ||
        syncing.value ||
        reconciling.value ||
        (Boolean(removingDirId.value) && (!selectedDir.value || selectedDir.value.dir_id === removingDirId.value))
)

const { percent: mountDirPct } = useLoadingProgressText(mountingDir, undefined, 1200)
const { percent: removeDirPct } = useLoadingProgressText(() => Boolean(removingDirId.value), undefined, 900)
const { percent: bgSyncPct } = useLoadingProgressText(syncing, undefined, 1200)
const { percent: bgReconcilePct } = useLoadingProgressText(reconciling, undefined, 1200)

const { text: dirsLoadingText } = useLoadingProgressText(
    dirsTableLoading,
    () => {
        if (removingDirId.value) return t('plugin.rolling-memory.background.removing')
        if (mountingDir.value) return t('plugin.rolling-memory.background.mounting')
        if (syncing.value) return t('plugin.rolling-memory.background.syncingLoading')
        if (reconciling.value) return t('plugin.rolling-memory.background.reconcilingLoading')
        return undefined
    }
)
const { text: filesLoadingText } = useLoadingProgressText(
    filesTableLoading,
    () => {
        if (removingDirId.value) return t('plugin.rolling-memory.background.removing')
        if (mountingDir.value) return t('plugin.rolling-memory.background.mounting')
        if (syncing.value) return t('plugin.rolling-memory.background.syncingLoading')
        if (reconciling.value) return t('plugin.rolling-memory.background.reconcilingLoading')
        return undefined
    }
)

const dirsTableRef = ref<any>(null)
const filesTableRef = ref<any>(null)
const dirs = ref<any[]>([])
const files = ref<any[]>([])
const filePage = ref(1)
const filePageSize = 20
const pagedFiles = computed(() => {
    const start = (filePage.value - 1) * filePageSize
    return files.value.slice(start, start + filePageSize)
})
const fileKeyword = ref('')
const selectedDir = ref<any>(null)
const selectedRelPath = ref<string>('')
const stats = ref<any>({})
const envHealth = ref<any>({})
const browserRef = ref<any>(null)

// 手动输入路径 / 编辑目录文档类型过滤对话框状态
const manualDialogVisible = ref(false)
const manualDialogMode = ref<'add' | 'edit'>('add')
const manualDialogPath = ref('')
const manualDialogExtTokens = ref<string[]>(['.md'])

const applyDialogExtPreset = (preset: 'md' | 'docs' | 'all') => {
    if (preset === 'md') {
        manualDialogExtTokens.value = ['.md']
    } else if (preset === 'docs') {
        manualDialogExtTokens.value = [...COMMON_DOC_TOKENS]
    } else {
        manualDialogExtTokens.value = [...allOptionTokens.value]
    }
}

const onDialogExtTokensChange = (val: any[]) => {
    if (!Array.isArray(val) || val.length === 0) {
        manualDialogExtTokens.value = ['.md']
        ElMessage.warning(t('plugin.rolling-memory.background.extEmptyWarn'))
    }
}

const handleToggleAutoSync = async (val: string | number | boolean) => {
    if (togglingAutoSync.value) return
    const enabled = Boolean(val)
    togglingAutoSync.value = true
    try {
        await request<any>({
            url: '/admin/plugin/rolling-memory/toggle',
            method: 'POST',
            data: { key: 'cloud_auto_sync', value: enabled ? 'true' : 'false' },
        })
        envHealth.value.auto_sync_enabled = enabled
        envHealth.value.sync_mode = enabled ? 'auto' : 'manual'
        ElMessage.success(
            enabled ? t('plugin.rolling-memory.overview.syncModeSwitchedAuto') : t('plugin.rolling-memory.overview.syncModeSwitchedManual')
        )
    } catch {
        envHealth.value.auto_sync_enabled = !enabled
    } finally {
        togglingAutoSync.value = false
    }
}

const selectedDirId = computed(() => selectedDir.value?.dir_id || '')

const bgAlign = computed(() => envHealth.value?.layers?.background || {})
const isLocalMode = computed(() => envHealth.value?.mode !== 'cloud')
const alignmentKnown = computed(() =>
    Boolean(
        envHealth.value?.layers?.background && (isLocalMode.value || (envHealth.value?.mode === 'cloud' && envHealth.value?.cloud_online === true))
    )
)

const onStatsLoaded = (payload: any) => {
    if (payload) {
        stats.value = payload
    }
}

const onUnitsDeleted = () => {
    loadDirs()
    loadFiles()
    refreshEnvHealth()
}

const loadEnvHealth = async () => {
    try {
        const res = await request<any>(
            { url: '/admin/plugin/rolling-memory/env/health', method: 'POST', data: {}, timeout: 0 },
            { cancelDuplicate: false, showErrorMessage: false }
        )
        envHealth.value = res.data?.data || {}
    } catch {
        envHealth.value = {}
    }
}

// 动作后刷新：页面已订阅实时流，强制采集排入共享流（数据帧走同一条 SSE）；
// 端点不可用（旧服务端）或流未连时回退一次性取值。
const refreshEnvHealth = async () => {
    try {
        const res = await request<any>(
            { url: '/admin/plugin/rolling-memory/env/health/refresh', method: 'POST', data: {}, timeout: 0 },
            { cancelDuplicate: false, showErrorMessage: false }
        )
        const data = res.data?.data
        if (data && (data.harnesses || data.layers)) {
            envHealth.value = data
        }
    } catch {
        await loadEnvHealth()
    }
}

// 环境与记忆健康实时流：走同源 MonitorSocket WebSocket 共享房间（rm/env_health）。
const startEnvHealthStream = () => {
    stopEnvHealthStream()
    const token = adminInfo.token
    if (!token) return
    envSocket = new MonitorSocket({
        token,
        room: 'rm/env_health',
        onMessage: (env: any) => {
            if (env?.event !== 'env_health' || !env.data) return
            const d = env.data
            if (d && (d.harnesses || d.layers)) {
                envHealth.value = d
            }
        },
    })
    envSocket.start()
}

const stopEnvHealthStream = () => {
    if (envSocket) {
        envSocket.stop()
        envSocket = null
    }
}

const loadDirs = async () => {
    loadingDirs.value = true
    try {
        const res = await request<any>({ url: '/admin/plugin/rolling-memory/background/dirs', method: 'POST', data: {} })
        dirs.value = res.data?.data || []
        if (selectedDir.value) {
            const still = dirs.value.find((d) => d.dir_id === selectedDir.value.dir_id)
            selectedDir.value = still || null
            void nextTick(() => {
                dirsTableRef.value?.setCurrentRow(selectedDir.value || null)
            })
        }
    } catch {
        // 业务与网络错误由请求拦截器统一提示。
    } finally {
        loadingDirs.value = false
    }
}

let fileKeywordTimer: ReturnType<typeof setTimeout> | null = null
const onFileKeywordInput = () => {
    if (fileKeywordTimer) clearTimeout(fileKeywordTimer)
    fileKeywordTimer = setTimeout(() => {
        loadFiles()
    }, 280)
}

const loadFiles = async () => {
    loadingFiles.value = true
    try {
        const res = await request<any>({
            url: '/admin/plugin/rolling-memory/background/files',
            method: 'POST',
            data: { dir_id: selectedDirId.value, keyword: fileKeyword.value, limit: 300 },
        })
        files.value = res.data?.data || []
        filePage.value = 1
        if (selectedRelPath.value) {
            const matched = files.value.find((f) => f.rel_path === selectedRelPath.value)
            void nextTick(() => {
                filesTableRef.value?.setCurrentRow(matched || null)
            })
        }
    } catch {
        // 业务与网络错误由请求拦截器统一提示。
    } finally {
        loadingFiles.value = false
    }
}

const selectDir = (row: any | null) => {
    selectedDir.value = row
    selectedRelPath.value = ''
    void nextTick(() => {
        dirsTableRef.value?.setCurrentRow(row || null)
        filesTableRef.value?.setCurrentRow(null)
    })
    loadFiles()
}

const onDirRowClick = (row: any) => {
    if (!row) return
    if (selectedDir.value?.dir_id === row.dir_id) {
        selectDir(null)
    } else {
        selectDir(row)
    }
}

const clearSelectedFile = () => {
    selectedRelPath.value = ''
    void nextTick(() => {
        filesTableRef.value?.setCurrentRow(null)
    })
}

const focusFileChunks = (row: any) => {
    if (!row?.rel_path) return
    if (selectedRelPath.value === row.rel_path) {
        clearSelectedFile()
        return
    }
    selectedRelPath.value = row.rel_path
    void nextTick(() => {
        filesTableRef.value?.setCurrentRow(row)
    })
}

const reloadAll = () => {
    loadDirs()
    loadFiles()
    refreshEnvHealth()
    browserRef.value?.reload()
}

const mountDirectory = async (path: string, tokensOverride?: string[]): Promise<boolean> => {
    if (!path.trim() || mountingDir.value) return false
    const tokens = tokensOverride?.length ? tokensOverride : selectedExtTokens.value
    const includeExts = expandTokensToExts(tokens)
    mountingDir.value = true
    try {
        const res = await request<any>({
            url: '/admin/plugin/rolling-memory/background/add',
            method: 'POST',
            data: { path: path.trim(), include_exts: includeExts },
            timeout: 0,
        })
        if (res.data?.code === 0) {
            const syncRep = res.data?.data?.sync
            if (syncRep && typeof syncRep.scanned_files === 'number') {
                ElMessage.success(
                    t('plugin.rolling-memory.background.mountSuccessDetail', {
                        scanned: syncRep.scanned_files ?? 0,
                        ingested: syncRep.ingested_files ?? 0,
                        chunks: syncRep.ingested_chunks ?? 0,
                    })
                )
            } else {
                ElMessage.success(t('plugin.rolling-memory.background.mountSuccess'))
            }
            if (res.data?.data?.notice) {
                ElMessage.info(res.data.data.notice)
            }
            reloadAll()
            return true
        }
        ElMessage.error(res.data?.message || t('plugin.rolling-memory.background.mountFailed'))
        return false
    } catch {
        // 业务与网络错误由请求拦截器统一提示。
        return false
    } finally {
        mountingDir.value = false
    }
}

const openManualMountDialog = (detail = '') => {
    const fallback = t('plugin.rolling-memory.background.manualFallback')
    if (detail) {
        ElMessage.warning({
            message: `${fallback}: ${detail}`,
            duration: 6000,
            showClose: true,
        })
    }
    manualDialogMode.value = 'add'
    manualDialogPath.value = ''
    manualDialogExtTokens.value = [...selectedExtTokens.value]
    manualDialogVisible.value = true
}

const openEditDirFilterDialog = (row: any) => {
    if (!row?.root_path || isBgTaskBusy.value) return
    manualDialogMode.value = 'edit'
    manualDialogPath.value = row.root_path
    manualDialogExtTokens.value = storedExtsToTokens(row.include_exts)
    manualDialogVisible.value = true
}

const submitManualDialog = async () => {
    const trimmed = manualDialogPath.value.trim()
    if (!trimmed || mountingDir.value) return
    selectedExtTokens.value = [...manualDialogExtTokens.value]
    const ok = await mountDirectory(trimmed, manualDialogExtTokens.value)
    if (ok) {
        manualDialogVisible.value = false
    }
}

const handleAddDir = async () => {
    if (isBgTaskBusy.value) return
    pickingDir.value = true
    try {
        const res = await request<any>({
            url: '/admin/plugin/rolling-memory/background/select',
            method: 'POST',
            data: {},
            timeout: 0,
        })
        const selection = res.data?.data
        switch (selection?.status) {
            case 'selected':
                pickingDir.value = false
                await mountDirectory(selection.path || '')
                break
            case 'cancelled':
                ElMessage.info(t('plugin.rolling-memory.background.cancelled'))
                break
            case 'busy':
                ElMessage.warning(t('plugin.rolling-memory.background.pickerBusy'))
                break
            default:
                pickingDir.value = false
                openManualMountDialog(selection?.detail || '')
                break
        }
    } catch {
        // 鉴权、业务与网络错误由请求拦截器统一提示。
    } finally {
        pickingDir.value = false
    }
}

const handleRemoveDir = async (row: any) => {
    if (!row || isBgTaskBusy.value) return
    try {
        await ElMessageBox.confirm(
            t('plugin.rolling-memory.background.removeConfirm', { path: row.root_path }),
            t('plugin.rolling-memory.background.removeConfirmTitle'),
            {
                type: 'warning',
                confirmButtonText: t('plugin.rolling-memory.background.removeBtn'),
                cancelButtonText: t('common.cancel'),
            }
        )
    } catch {
        return
    }
    removingDirId.value = row.dir_id || row.root_path
    try {
        const res = await request<any>({
            url: '/admin/plugin/rolling-memory/background/remove',
            method: 'POST',
            data: { dir_id: row.dir_id, path: row.root_path },
            timeout: 0,
        })
        const removedChunks = Number(res.data?.data?.removed_chunks ?? 0)
        ElMessage.success(t('plugin.rolling-memory.background.removeSuccessDetail', { chunks: removedChunks }))
        const keptFiles = Number(res.data?.data?.kept_files || 0)
        if (keptFiles > 0) {
            ElMessage.info(t('plugin.rolling-memory.background.removeKeptFiles', { count: keptFiles }))
        }
        if (selectedDir.value?.dir_id === row.dir_id) {
            selectedDir.value = null
            selectedRelPath.value = ''
        }
        reloadAll()
    } catch {
        // 业务与网络错误由请求拦截器统一提示。
    } finally {
        removingDirId.value = ''
    }
}

const handleSync = async () => {
    if (isBgTaskBusy.value) return
    syncing.value = true
    try {
        const res = await request<any>({ url: '/admin/plugin/rolling-memory/background/sync', method: 'POST', data: {}, timeout: 0 })
        const rep = res.data?.data
        if (rep) {
            ElMessage.success(
                t('plugin.rolling-memory.background.syncSummary', {
                    scanned: rep.scanned_files,
                    ingested: rep.ingested_files,
                    unchanged: rep.unchanged_files,
                })
            )
        } else {
            ElMessage.warning(t('plugin.rolling-memory.overview.reconcileNoReport'))
        }
        reloadAll()
    } catch {
        // 业务与网络错误由请求拦截器统一提示。
    } finally {
        syncing.value = false
    }
}

const handleReconcileBg = async () => {
    if (isBgTaskBusy.value) return
    reconciling.value = true
    try {
        const res = await request<any>({
            url: '/admin/plugin/rolling-memory/sync/reconcile',
            method: 'POST',
            data: { layer: 'background', limit: 100 },
            timeout: 0,
        })
        const rep = res.data?.data
        if (rep) {
            const summary = isLocalMode.value
                ? t('plugin.rolling-memory.background.reconcileSuccessLocal', {
                      bg: rep.background_synced_files ?? 0,
                  })
                : t('plugin.rolling-memory.background.reconcileResult', {
                      files: rep.background_synced_files ?? 0,
                      chunks: rep.background_pushed_to_remote ?? 0,
                  })
            const actionableWarnings = (rep.warnings || []).filter(
                (w: string) => !String(w).includes('episodic index backfill covers only the scanned harness window')
            )
            if (actionableWarnings.length) {
                ElMessage.warning({
                    message: `${summary}\n${t('plugin.rolling-memory.overview.reconcileWarnings')} ${actionableWarnings.join('; ')}`,
                    duration: 0,
                    showClose: true,
                })
            } else {
                ElMessage.success(summary)
            }
            if (rep.status && (rep.status.harnesses || rep.status.layers)) {
                envHealth.value = { ...envHealth.value, ...rep.status }
                loadDirs()
                loadFiles()
                browserRef.value?.reload()
            } else {
                reloadAll()
            }
        } else {
            ElMessage.warning(t('plugin.rolling-memory.overview.reconcileNoReport'))
            refreshEnvHealth()
        }
    } catch {
        // 业务与网络错误由请求拦截器统一提示。
    } finally {
        reconciling.value = false
    }
}

onMounted(() => {
    loadDirs()
    loadFiles()
    // 首屏只订阅实时流：服务端共享 watcher 推帧即达；显式全量诊断保留给动作后的 loadEnvHealth。
    startEnvHealthStream()
})

let bgDeactivated = false
onActivated(() => {
    if (!bgDeactivated) return
    bgDeactivated = false
    startEnvHealthStream()
})

onDeactivated(() => {
    bgDeactivated = true
    if (fileKeywordTimer) {
        clearTimeout(fileKeywordTimer)
        fileKeywordTimer = null
    }
    stopEnvHealthStream()
})

onBeforeUnmount(() => {
    if (fileKeywordTimer) {
        clearTimeout(fileKeywordTimer)
        fileKeywordTimer = null
    }
    stopEnvHealthStream()
})
</script>

<style scoped lang="scss">
.rm-alert {
    margin-bottom: 10px;
}
.rm-stats-bar {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 10px;
    margin-bottom: 10px;
}
@media (max-width: 960px) {
    .rm-stats-bar {
        grid-template-columns: repeat(2, minmax(0, 1fr));
    }
}
.stat-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 14px;
    border: 1px solid var(--el-border-color-lighter);
    border-left: 3px solid var(--el-color-primary);
    border-radius: 6px;
    background: var(--ag-bg-color-overlay);
    &.success {
        border-left-color: var(--el-color-success);
    }
    &.warning {
        border-left-color: var(--el-color-warning);
    }
    &.info {
        border-left-color: var(--el-color-info);
    }
}
.stat-label {
    font-size: 12px;
    color: var(--el-text-color-secondary);
}
.stat-value {
    font-size: 16px;
    font-weight: 700;
    color: var(--el-text-color-primary);
}
.mb-10 {
    margin-bottom: 10px;
}
.sub-table-box {
    background-color: var(--ag-bg-color-overlay);
    border: 1px solid var(--ag-border-color);
    border-radius: var(--el-border-radius-base);
    padding: 12px;
    height: 100%;
}
.sub-table-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    margin-bottom: 6px;
}
.sub-table-title {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    font-size: 13px;
    font-weight: 600;
    color: var(--el-text-color-primary);
}
.sub-table-actions {
    display: flex;
    align-items: center;
    gap: 6px;
}
.sub-table-hint {
    font-size: 12px;
    color: var(--el-text-color-secondary);
    margin-bottom: 8px;
    line-height: 1.4;
}
.ext-filter-bar {
    padding: 6px 10px;
    margin-bottom: 8px;
    border-radius: 6px;
    border: 1px solid var(--el-border-color-lighter);
    background: var(--el-fill-color-blank);
}
.ext-filter-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 6px;
    margin-bottom: 2px;
}
.ext-filter-label {
    font-size: 12px;
    font-weight: 600;
    color: var(--el-text-color-regular);
}
.ext-filter-presets {
    display: flex;
    align-items: center;
    gap: 6px;
}
.ext-checkbox-group {
    display: flex;
    flex-wrap: wrap;
    gap: 2px 12px;
    :deep(.el-checkbox) {
        margin-right: 0;
        height: 22px;
    }
    :deep(.el-checkbox__label) {
        font-family: monospace;
        font-size: 12px;
        padding-left: 5px;
    }
}
.dir-ext-tag {
    cursor: pointer;
    font-family: monospace;
    max-width: 96px;
    overflow: hidden;
    text-overflow: ellipsis;
}
.manual-mount-dialog-body {
    display: flex;
    flex-direction: column;
    gap: 12px;
}
.manual-dialog-desc {
    margin: 0;
    font-size: 13px;
    line-height: 1.5;
    color: var(--el-text-color-regular);
}
.dialog-ext-bar {
    margin-bottom: 0;
}
.btn-icon {
    margin-right: 4px;
}
.btns-ml-12 {
    margin-left: 12px;
}
.table-header-operate-text {
    margin-left: 6px;
}
.filter-tag {
    margin-right: 4px;
}
.files-pager {
    display: flex;
    justify-content: flex-end;
    padding-top: 6px;
}
</style>
