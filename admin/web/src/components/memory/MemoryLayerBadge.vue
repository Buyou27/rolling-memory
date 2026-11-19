<!--
  MemoryLayerBadge —— L1 Background / L2 Episodic / L3 Noetic 的统一图层徽章。
  色值来自 @/utils/memoryLayers，禁止在调用点另写三层色值。
-->
<template>
    <span v-if="resolvedLabel" class="rm-layer-badge" :class="[layerKey ? `is-${layerKey}` : 'is-unknown', `is-${size}`]" :title="title">
        <i class="rm-layer-badge-dot" aria-hidden="true" />
        <span class="rm-layer-badge-text">{{ resolvedLabel }}</span>
    </span>
</template>

<script setup lang="ts">
import { normalizeMemoryLayer } from '@/utils/memoryLayers'
import { computed } from 'vue'

const props = withDefaults(
    defineProps<{
        /** 图层键：background / episodic / noetic（兼容 l1 / l2 / l3）。 */
        layer?: string
        /** 覆盖默认文案；默认使用图层键大写。 */
        label?: string
        size?: 'small' | 'default'
        /** BuildAdmin 表格 customRender 传入的单元格值。 */
        renderValue?: string
        /** BuildAdmin 表格 customRender 传入的列配置（含 dict 文案映射）。 */
        renderColumnConfig?: { dict?: Record<string, string> }
        /** 以下为 BuildAdmin 表格 customRender 的上下文参数，声明后避免落到根节点属性上。 */
        renderRow?: unknown
        renderColumn?: unknown
        renderIndex?: number
        manager?: unknown
    }>(),
    {
        layer: '',
        label: '',
        size: 'small',
        renderValue: '',
        renderColumnConfig: undefined,
        renderRow: undefined,
        renderColumn: undefined,
        renderIndex: undefined,
        manager: undefined,
    }
)

const layerKey = computed(() => normalizeMemoryLayer(props.layer || props.renderValue))
const rawValue = computed(() => String(props.layer || props.renderValue || ''))
const dictLabel = computed(() => {
    if (props.renderColumnConfig?.dict) return props.renderColumnConfig.dict[rawValue.value] || ''
    return ''
})
const resolvedLabel = computed(() => {
    if (props.label) return props.label
    if (dictLabel.value) return dictLabel.value
    if (layerKey.value) return layerKey.value.toUpperCase()
    return rawValue.value.toUpperCase()
})
const title = computed(() => {
    if (!layerKey.value) return resolvedLabel.value
    const names: Record<string, string> = {
        background: 'L1 Background',
        episodic: 'L2 Episodic',
        noetic: 'L3 Noetic',
    }
    return `${names[layerKey.value]} · ${resolvedLabel.value}`
})
</script>

<style scoped lang="scss">
.rm-layer-badge {
    --rm-badge-color: var(--el-text-color-regular);
    --rm-badge-rgb: 144, 147, 153;

    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 0 6px;
    border: 1px solid rgba(var(--rm-badge-rgb), 0.42);
    border-radius: 4px;
    background: rgba(var(--rm-badge-rgb), 0.12);
    color: var(--rm-badge-color);
    font-size: 10px;
    font-weight: 700;
    line-height: 18px;
    letter-spacing: 0.02em;
    white-space: nowrap;
    vertical-align: middle;

    &.is-small {
        font-size: 10px;
        line-height: 18px;
    }

    &.is-default {
        padding: 0 8px;
        font-size: 12px;
        line-height: 22px;
    }

    &.is-background {
        --rm-badge-color: var(--rm-layer-background);
        --rm-badge-rgb: var(--rm-layer-background-rgb);
    }

    &.is-episodic {
        --rm-badge-color: var(--rm-layer-episodic);
        --rm-badge-rgb: var(--rm-layer-episodic-rgb);
    }

    &.is-noetic {
        --rm-badge-color: var(--rm-layer-noetic);
        --rm-badge-rgb: var(--rm-layer-noetic-rgb);
    }

    .rm-layer-badge-dot {
        width: 6px;
        height: 6px;
        flex: none;
        border-radius: 50%;
        background: currentColor;
        box-shadow: 0 0 5px currentColor;
    }
}
</style>
