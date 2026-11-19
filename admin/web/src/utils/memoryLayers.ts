/**
 * 记忆三层（L1 Background / L2 Episodic / L3 Noetic）唯一调色板。
 *
 * 图表、图例、图元、徽章与 3D 引擎统一从这里取色；组件内不再写死三层色值。
 * 浏览器侧同时把色板写入 `--rm-layer-*` CSS 变量，供 scoped 样式引用。
 */
export type MemoryLayerKey = 'background' | 'episodic' | 'noetic'

export interface MemoryLayerPalette {
    /** 主色（ECharts / Canvas / 行内样式） */
    hex: string
    /** 主色 RGB 分量（CSS rgba() / WebGL） */
    rgb: string
    /** 深色（发光外圈、自发光） */
    deep: string
    /** 浅色（高亮描边） */
    light: string
}

export const MEMORY_LAYER_COLORS: Record<MemoryLayerKey, MemoryLayerPalette> = {
    background: { hex: '#3b82f6', rgb: '59, 130, 246', deep: '#1d4ed8', light: '#60a5fa' },
    episodic: { hex: '#10b981', rgb: '16, 185, 129', deep: '#047857', light: '#34d399' },
    noetic: { hex: '#ec4899', rgb: '236, 72, 153', deep: '#be185d', light: '#f472b6' },
}

const CSS_VAR_PREFIX = '--rm-layer'

/** 把任意层名 / 简称归一化为三层键；未知值返回 null。 */
export function normalizeMemoryLayer(raw: unknown): MemoryLayerKey | null {
    const value = String(raw ?? '')
        .trim()
        .toLowerCase()
    if (value === 'background' || value === 'l1') return 'background'
    if (value === 'episodic' || value === 'l2') return 'episodic'
    if (value === 'noetic' || value === 'l3') return 'noetic'
    return null
}

/** 层主色；未知层回落到 fallback。 */
export function memoryLayerColor(raw: unknown, fallback = '#94a3b8'): string {
    const key = normalizeMemoryLayer(raw)
    return key ? MEMORY_LAYER_COLORS[key].hex : fallback
}

/** 层简称（L1 / L2 / L3）；未知层返回空串。 */
export function memoryLayerShortLabel(raw: unknown): string {
    const key = normalizeMemoryLayer(raw)
    if (key === 'background') return 'L1'
    if (key === 'episodic') return 'L2'
    if (key === 'noetic') return 'L3'
    return ''
}

/** 十六进制色转 rgba() 字符串。 */
export function hexToRgba(hex: string, alpha: number): string {
    const value = hex.replace('#', '')
    const normalized = value.length === 3 ? value.replace(/./g, (char) => char + char) : value
    const numeric = Number.parseInt(normalized, 16)
    const r = (numeric >> 16) & 255
    const g = (numeric >> 8) & 255
    const b = numeric & 255
    return `rgba(${r}, ${g}, ${b}, ${alpha})`
}

/** 把三层色板写入 CSS 变量；重复调用幂等。 */
export function installMemoryLayerCssVars(root?: HTMLElement): void {
    if (typeof document === 'undefined') return
    const target = root ?? document.documentElement
    for (const [key, palette] of Object.entries(MEMORY_LAYER_COLORS) as [MemoryLayerKey, MemoryLayerPalette][]) {
        target.style.setProperty(`${CSS_VAR_PREFIX}-${key}`, palette.hex)
        target.style.setProperty(`${CSS_VAR_PREFIX}-${key}-rgb`, palette.rgb)
        target.style.setProperty(`${CSS_VAR_PREFIX}-${key}-deep`, palette.deep)
        target.style.setProperty(`${CSS_VAR_PREFIX}-${key}-light`, palette.light)
    }
}

// 组件模块首次加载时写入 CSS 变量，保证 scoped 样式在首帧即可取到色值。
if (typeof document !== 'undefined') installMemoryLayerCssVars()
