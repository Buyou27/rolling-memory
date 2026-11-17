import { strict as assert } from 'node:assert'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { test } from 'node:test'

const require = createRequire(import.meta.url)
const ts = require('typescript')
const source = readFileSync(new URL('./MemoryField.vue', import.meta.url), 'utf8')
const start = source.indexOf('function renderChart(')
const end = source.indexOf('\nconst layoutInput', start)
assert.ok(start >= 0 && end > start)
const compiled = ts.transpileModule(source.slice(start, end), {
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS },
}).outputText

test('successful graph followed by a failed load clears rendered nodes and edges', () => {
    assert.match(source, /catch\s*\{\s*if \(revision !== graphRevision\) return\s*graph\.value = null[\s\S]*?await nextTick\(\)\s*if \(revision === graphRevision\) renderChart\(true\)/)
    assert.match(source, /if \(!inp \|\| inp\.nodes\.length === 0\) \{\s*layoutSeq\+\+/)
    const calls = []
    const ctrl = {
        resize() {},
        setData(input) {
            calls.push(input)
        },
    }
    const view = { value: { nodes: [{ id: 1 }], edges: [{ from: 1, to: 1 }] } }
    const layout = { value: { 1: [0, 0] } }
    const render = new Function(
        'ensureThreeEngine',
        'threeContainerRef',
        'inDrill',
        'activeView',
        'egoResult',
        'layout',
        'spaceMode',
        'drillFocus',
        'selectedNodeId',
        'showEdgeLabels',
        `${compiled}; return renderChart`
    )(
        () => ctrl,
        { value: null },
        { value: false },
        view,
        { value: null },
        layout,
        { value: 'sphere' },
        { value: null },
        { value: null },
        { value: false }
    )
    render(true)
    view.value = { nodes: [], edges: [] }
    layout.value = {}
    render(true)
    assert.equal(calls.length, 2)
    assert.equal(calls[0].nodes.length, 1)
    assert.deepEqual(calls[1].nodes, [])
    assert.deepEqual(calls[1].edges, [])
    view.value = { nodes: [{ id: 2 }], edges: [] }
    layout.value = { 2: [0, 0] }
    render(true)
    assert.equal(calls[2].nodes[0].id, 2)
})

test('homepage uses the LoCoMo sample graph endpoints as same-source preview with admin graph', () => {
    assert.match(source, /\/plugin\/rolling-memory\/locomo\/graph/)
    assert.match(source, /\/plugin\/rolling-memory\/locomo\/expand/)
    assert.match(source, /\/plugin\/rolling-memory\/locomo\/recall/)
    assert.match(source, /LoCoMo/)
    assert.match(source, /revision !== graphRevision/)
})

test('load clears stale graph on failure or empty response and can recover', async () => {
    const first = source.indexOf('let graphRevision = 0')
    const last = source.indexOf('\nasync function drillInto', first)
    assert.ok(first >= 0 && last > first)
    const script = ts.transpileModule(source.slice(first, last), {
        compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS },
    }).outputText
    const graph = { value: null }
    const questions = { value: [] }
    const active = { value: '' }
    const receipt = { value: null }
    const nodes = new Map()
    let response
    let fail = false
    const renders = []
    const load = new Function(
        'request', 'inDrill', 'exitDrill', 'loading', 'nodeLimit', 'keyword', 'graph', 'lastRecallResult',
        'nodeByKey', 'locomoQuestions', 'activeQuestionId', 'nextTick', 'renderChart', 'selectedNodeId',
        'hover3DNode', 'detailVisible', 'currentNode', 'recalling', `${script}; return load`
    )(
        async () => {
            if (fail) throw new Error('offline')
            return { data: { data: response } }
        },
        { value: false }, () => {}, { value: false }, { value: 256 }, { value: '' }, graph, receipt,
        nodes, questions, active, async () => {}, () => renders.push(graph.value), { value: null },
        { value: null }, { value: false }, { value: null }, { value: false }
    )
    response = {
        source: 'open-memory-v1', scope_key: 'demo-scope', scopes: [],
        nodes: [{ id: 1, node_key: 'unit:locomo-1', summary: 'sample', kind: 'unit', layer: 'episodic', weight: 1,
            certainty: '', intention: '', status: 'active', confidence: 0, observed_at: '', valid_from: '', valid_to: '',
            anchor: 'unit:locomo-1', span: 'sample text', aliases: [], degree: 0 }],
        edges: [], relations: [], questions: [{ id: 'q1', question: 'Q?', answer: 'A' }],
        stats: { event_total: 1, link_total: 0, node_shown: 1, edge_shown: 0, isolated_shown: 1 },
        truncated: { nodes: false, edges: false },
    }
    await load()
    assert.equal(nodes.size, 1)
    assert.equal(active.value, 'q1')
    fail = true
    await load()
    assert.equal(graph.value, null)
    assert.equal(nodes.size, 0)
    assert.deepEqual(questions.value, [])
    assert.equal(renders.at(-1), null)
    fail = false
    response = { nodes: [], edges: [] }
    await load()
    assert.equal(graph.value, null)
    response = {
        source: 'open-memory-v1', scope_key: 'demo-scope', scopes: [],
        nodes: [{ id: 2, node_key: 'unit:restored', summary: 'restored', kind: 'unit', layer: 'episodic', weight: 1,
            certainty: '', intention: '', status: 'active', confidence: 0, observed_at: '', valid_from: '', valid_to: '',
            anchor: 'unit:restored', span: 'sample text', aliases: [], degree: 0 }],
        edges: [], relations: [], questions: [],
        stats: { event_total: 1, link_total: 0, node_shown: 1, edge_shown: 0, isolated_shown: 1 },
        truncated: { nodes: false, edges: false },
    }
    await load()
    assert.equal(graph.value.nodes[0].node_key, 'unit:restored')
    assert.equal(nodes.size, 1)
})

test('capacity requests retain only the latest response and loading state', async () => {
    const first = source.indexOf('function selectNodeCapacity(')
    const last = source.indexOf('\nconst graphPageRef', first)
    const loadStart = source.indexOf('let graphRevision = 0')
    const loadEnd = source.indexOf('\nasync function drillInto', loadStart)
    assert.ok(first >= 0 && last > first && loadStart >= 0 && loadEnd > loadStart)
    const script = ts.transpileModule(`${source.slice(first, last)}\n${source.slice(loadStart, loadEnd)}`, {
        compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS },
    }).outputText
    const pending = []
    const graph = { value: null }
    const loading = { value: false }
    const nodeLimit = { value: 256 }
    const renders = []
    const state = new Function(
        'request', 'inDrill', 'exitDrill', 'loading', 'nodeLimit', 'keyword', 'graph', 'lastRecallResult',
        'nodeByKey', 'locomoQuestions', 'activeQuestionId', 'nextTick', 'renderChart', 'selectedNodeId',
        'hover3DNode', 'detailVisible', 'currentNode', 'recalling',
        `${script}; return { load, selectNodeCapacity }`
    )(
        (options) => new Promise((resolve, reject) => pending.push({ options, resolve, reject })),
        { value: false }, () => {}, loading, nodeLimit, { value: '' }, graph, { value: null },
        new Map(), { value: [] }, { value: '' }, async () => {}, () => renders.push(graph.value),
        { value: null }, { value: null }, { value: false }, { value: null }, { value: false }
    )
    const firstLoad = state.load()
    state.selectNodeCapacity(128)
    assert.equal(pending.length, 2)
    assert.equal(pending[0].options.data.limit, 256)
    assert.equal(pending[1].options.data.limit, 128)
    const payload = (key) => ({ nodes: [{ node_key: key }], edges: [], relations: [], stats: {}, questions: [] })
    pending[1].resolve({ data: { data: payload('unit:new') } })
    await new Promise((resolve) => setImmediate(resolve))
    assert.equal(graph.value.nodes[0].node_key, 'unit:new')
    assert.equal(loading.value, false)
    pending[0].resolve({ data: { data: payload('unit:old') } })
    await firstLoad
    assert.equal(graph.value.nodes[0].node_key, 'unit:new')
    assert.equal(renders.length, 1)

    const staleFailure = state.load()
    state.selectNodeCapacity(512)
    pending[2].reject(new Error('stale failure'))
    await staleFailure
    assert.equal(loading.value, true)
    assert.equal(graph.value.nodes[0].node_key, 'unit:new')
    pending[3].resolve({ data: { data: payload('unit:latest') } })
    await new Promise((resolve) => setImmediate(resolve))
    assert.equal(graph.value.nodes[0].node_key, 'unit:latest')
    assert.equal(loading.value, false)
})
