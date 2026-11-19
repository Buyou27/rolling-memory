import { strict as assert } from 'node:assert'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { test } from 'node:test'

const require = createRequire(import.meta.url)
const ts = require('typescript')
const source = readFileSync(new URL('./layout.ts', import.meta.url), 'utf8')
const javascript = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText
const module = { exports: {} }
new Function('exports', 'require', 'module', javascript)(module.exports, require, module)
const { fibonacciSpherePoint } = module.exports

test('fibonacci sphere samples stay exactly on the requested sphere', () => {
    for (const total of [2, 7, 40, 48]) {
        for (let idx = 0; idx < total * 4; idx++) {
            const [x, y, z] = fibonacciSpherePoint(idx, total, 165, 2.1)
            const radius = Math.hypot(x, y, z)
            // |point| === radius：早期固定分母实现会在 idx >= total 时把 z 拖到球外
            assert.ok(Math.abs(radius - 165) < 1e-6, `total=${total} idx=${idx} radius=${radius}`)
            assert.ok(z >= -165 - 1e-9 && z <= 165 + 1e-9, `total=${total} idx=${idx} z=${z}`)
        }
    }
    const [x, y, z] = fibonacciSpherePoint(0, 1, 165, 0)
    assert.equal(x, 0)
    assert.equal(y, 0)
    assert.equal(z, 165 * 0.35)
})

test('fibonacci sphere wraps oversized indexes instead of leaking outside', () => {
    const a = fibonacciSpherePoint(0, 40, 100, 0.4)
    const b = fibonacciSpherePoint(40, 40, 100, 0.4)
    assert.deepEqual(a, b)
})

test('fibonacci sphere handles degenerate totals', () => {
    const single = fibonacciSpherePoint(3, 1, 120)
    assert.equal(single[0], 0)
    assert.equal(single[1], 0)
    assert.equal(single[2], 120 * 0.35)
    const zero = fibonacciSpherePoint(0, 0, 80)
    assert.equal(Math.hypot(...zero).toFixed(6), (80 * 0.35).toFixed(6))
})
