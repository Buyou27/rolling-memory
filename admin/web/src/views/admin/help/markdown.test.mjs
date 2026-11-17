import { strict as assert } from 'node:assert'
import { Buffer } from 'node:buffer'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { createRequire } from 'node:module'

// Transpile the module and load it via a data: URL import — same no-build approach as eval.test.mjs.
const require = createRequire(import.meta.url)
const ts = require('typescript')
const source = readFileSync(new URL('./markdown.ts', import.meta.url), 'utf8')
const js = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } }).outputText
const { renderMarkdownDocument } = await import(`data:text/javascript;base64,${Buffer.from(js).toString('base64')}`)

const resolveAsset = (src) => (/^(https?:)?\/\//i.test(src) ? src : `LOCAL:${src}`)

test('image markdown goes through the injected resolver', () => {
    const out = renderMarkdownDocument('![shot](../assets/live-console.png)', false, resolveAsset)
    assert.match(out.html, /<img class="md-image" src="LOCAL:\.\.\/assets\/live-console\.png" alt="shot"/)
})

test('absolute image urls pass through unchanged', () => {
    const out = renderMarkdownDocument('![shot](https://example.com/x.png)', false, resolveAsset)
    assert.match(out.html, /<img class="md-image" src="https:\/\/example\.com\/x\.png"/)
})

test('without resolver the raw src is kept and links stay links', () => {
    const out = renderMarkdownDocument('![shot](assets/x.png)\n\n[FAQ](faq.md)', false)
    assert.match(out.html, /<img class="md-image" src="assets\/x\.png"/)
    assert.match(out.html, /data-internal-href="faq\.md"/)
    assert.ok(!out.html.includes('![shot]'))
})

test('indented fence after a bullet parses as a code block, not list text', () => {
    const md = [
        '- item: hooks can be disabled:',
        '  ```bash',
        '  cmd one',
        '',
        '  cmd two',
        '  ```',
        '',
        '> quote after',
        '',
        '![img](assets/x.png)',
    ].join('\n')
    const out = renderMarkdownDocument(md, false, resolveAsset)
    assert.match(out.html, /<ul class="md-list"><li>item: hooks can be disabled:<\/li><\/ul>/)
    assert.match(out.html, /<div class="md-code-block"[\s\S]*cmd one[\s\S]*cmd two[\s\S]*<\/div>/)
    assert.match(out.html, /<blockquote class="md-blockquote">/)
    assert.match(out.html, /<img class="md-image"/)
})
