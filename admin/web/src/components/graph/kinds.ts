/**
 * Shared memory-graph kind registry.
 *
 * The engine projection emits word / phrase / cluster / unit / free_unit; the
 * legacy LoCoMo preview still emits cell / atom / molecule. Both the 3D engine
 * and the console views read this registry so a new kind cannot drift between
 * the canvas, the legend and the filters.
 */
import { MEMORY_LAYER_COLORS, normalizeMemoryLayer } from '@/utils/memoryLayers'

export type GraphKind = 'cluster' | 'unit' | 'word' | 'phrase' | 'free_unit' | 'cell' | 'atom' | 'molecule'

export type ClusterType = 'episode' | 'entity' | 'source'

export interface KindMeta {
    labelZh: string
    labelEn: string
    color: string
    role: 'container' | 'unit' | 'word' | 'phrase'
}

export const KIND_META: Record<string, KindMeta> = {
    cluster: { labelZh: '簇', labelEn: 'Cluster', color: '#0d9488', role: 'container' },
    unit: { labelZh: '单元', labelEn: 'Unit', color: '#10b981', role: 'unit' },
    word: { labelZh: '词', labelEn: 'Word', color: '#f59e0b', role: 'word' },
    phrase: { labelZh: '短语', labelEn: 'Phrase', color: '#a855f7', role: 'phrase' },
    free_unit: { labelZh: '游离单元', labelEn: 'Free unit', color: '#94a3b8', role: 'unit' },
    cell: { labelZh: '记忆地址容器', labelEn: 'Memory address container', color: '#0891b2', role: 'container' },
    atom: { labelZh: '原子', labelEn: 'Atom', color: '#f59e0b', role: 'word' },
    molecule: { labelZh: '分子', labelEn: 'Molecule', color: '#a855f7', role: 'phrase' },
}

export const CLUSTER_TYPE_META: Record<string, { labelZh: string; labelEn: string; color: string }> = {
    episode: { labelZh: '时间簇', labelEn: 'Time episode', color: '#0d9488' },
    entity: { labelZh: '实体簇', labelEn: 'Entity cluster', color: '#6366f1' },
    source: { labelZh: '来源簇', labelEn: 'Source cluster', color: '#1d4ed8' },
}

export function isContainerKind(kind: string | undefined): boolean {
    return kind === 'cluster' || kind === 'cell'
}

export function isWordKind(kind: string | undefined): boolean {
    return kind === 'word' || kind === 'atom'
}

export function isPhraseKind(kind: string | undefined): boolean {
    return kind === 'phrase' || kind === 'molecule'
}

export function isUnitKind(kind: string | undefined): boolean {
    return kind === 'unit' || kind === 'free_unit'
}

export function kindMeta(kind: string | undefined): KindMeta {
    return KIND_META[kind || 'unit'] || KIND_META.unit
}

export function kindLabel(kind: string | undefined, isEn: boolean): string {
    const meta = kindMeta(kind)
    return isEn ? meta.labelEn : meta.labelZh
}

export function clusterTypeLabel(clusterType: string | undefined, isEn: boolean): string {
    const meta = CLUSTER_TYPE_META[clusterType || '']
    if (!meta) return isEn ? 'Cluster' : '簇'
    return isEn ? meta.labelEn : meta.labelZh
}

/** Accent used by the 2D legend/HUD and the 3D labels. */
export function nodeAccentColor(node: { kind?: string; layer?: string; cluster_type?: string }): string {
    if (isContainerKind(node.kind) && node.cluster_type) {
        return CLUSTER_TYPE_META[node.cluster_type]?.color || kindMeta(node.kind).color
    }
    if (node.kind === 'free_unit') return KIND_META.free_unit.color
    if (isWordKind(node.kind)) return KIND_META.word.color
    if (isPhraseKind(node.kind)) return KIND_META.phrase.color
    const layer = normalizeMemoryLayer(node.layer)
    if (layer) return MEMORY_LAYER_COLORS[layer].hex
    return kindMeta(node.kind).color
}
