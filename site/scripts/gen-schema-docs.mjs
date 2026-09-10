import { readFileSync, writeFileSync, mkdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const schemaDir = resolve(root, '..', 'schema', 'v1')
const outDir = resolve(root, 'docs')

const pages = [
  { file: 'manifest.json', out: 'manifest.md', intro: 'The project manifest. Hand-edited, committed, and read by every command.' },
  { file: 'lock.json', out: 'lock.md', intro: 'The lock file. Written by the CLI, committed alongside the manifest, never hand-edited.' },
]

const code = (s) => '`' + String(s).replace(/`/g, '\\`') + '`'
const escape = (s) => String(s ?? '').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/\{\{/g, '&#123;&#123;')
const cell = (s) => escape(s).replace(/\|/g, '\\|').replace(/\n+/g, ' ').trim()
const refName = (ref) => ref.replace(/^#\/\$defs\//, '')
const anchor = (name) => `[${code(name)}](#${name.toLowerCase()})`

function typeOf(s) {
  if (!s || typeof s !== 'object') return ''
  if (s.$ref) return anchor(refName(s.$ref))
  if (s.const !== undefined) return code(JSON.stringify(s.const))
  if (s.enum) return s.enum.map((v) => code(JSON.stringify(v))).join(' \\| ')
  for (const k of ['oneOf', 'anyOf']) if (s[k]) return s[k].map(typeOf).filter(Boolean).join(' \\| ')
  if (s.allOf) return s.allOf.map(typeOf).filter(Boolean).join(' & ')
  if (s.type === 'array') return `${typeOf(s.items) || 'any'}[]`
  if (s.type === 'object') {
    if (s.properties && !s.additionalProperties) return 'object'
    const v = typeOf(s.additionalProperties)
    return v ? `map of ${v}` : 'object'
  }
  if (Array.isArray(s.type)) return s.type.map(code).join(' \\| ')
  return s.type ? code(s.type) : ''
}

function conditionalTypes(s) {
  const found = {}
  for (const branch of s.allOf ?? []) {
    for (const [name, p] of Object.entries(branch.then?.properties ?? {})) {
      const t = typeOf(p)
      if (t) (found[name] ??= new Set()).add(t)
    }
  }
  return found
}

function constraints(s) {
  const out = []
  if (s.format) out.push(`format ${code(s.format)}`)
  if (s.pattern) out.push(`pattern ${code(s.pattern)}`)
  if (s.minLength !== undefined) out.push(`min length ${s.minLength}`)
  if (s.minimum !== undefined) out.push(`min ${s.minimum}`)
  if (s.maximum !== undefined) out.push(`max ${s.maximum}`)
  if (s.minItems !== undefined) out.push(`min items ${s.minItems}`)
  if (s.minProperties !== undefined) out.push(`min properties ${s.minProperties}`)
  if (s.uniqueItems) out.push('unique items')
  if (s.propertyNames?.pattern) out.push(`keys match ${code(s.propertyNames.pattern)}`)
  if (s.propertyNames?.$ref) out.push(`keys are ${anchor(refName(s.propertyNames.$ref))}`)
  if (s.default !== undefined) out.push(`default ${code(JSON.stringify(s.default))}`)
  return out
}

function propertyTable(s) {
  const props = s.properties ?? {}
  const names = Object.keys(props)
  if (names.length === 0) return ''
  const required = new Set(s.required ?? [])
  const conditional = conditionalTypes(s)
  const rows = names.map((name) => {
    const p = props[name]
    const type = typeOf(p) || [...(conditional[name] ?? [])].join(' \\| ')
    const desc = [cell(p.description), constraints(p).map(cell).join(', ')].filter(Boolean).join('<br>')
    return `| ${code(name)}${required.has(name) ? ' *' : ''} | ${type} | ${desc} |`
  })
  return ['| Property | Type | Description |', '| --- | --- | --- |', ...rows].join('\n')
}

function describe(s) {
  const lines = []
  if (s.description) lines.push(escape(s.description), '')
  const table = propertyTable(s)
  if (table) {
    lines.push(table, '')
    if (s.additionalProperties === false) lines.push('No other properties are allowed.', '')
  } else {
    const t = typeOf(s)
    const c = constraints(s)
    if (t) lines.push(`Type: ${t}${c.length ? '. ' + c.join(', ') : ''}`, '')
  }
  return lines
}

function render(schema, intro) {
  const lines = [
    '---',
    'editLink: false',
    '---',
    '',
    `# ${schema.title}`,
    '',
    intro,
    '',
    escape(schema.description),
    '',
    `Schema: [${schema.$id}](${new URL(schema.$id).pathname})`,
    '',
    '## Properties',
    '',
    'Required properties are marked with *.',
    '',
    ...describe({ ...schema, description: undefined }),
    '## Definitions',
    '',
  ]
  for (const [name, def] of Object.entries(schema.$defs ?? {})) {
    lines.push(`### ${name}`, '', ...describe(def))
  }
  return lines.join('\n').replace(/\n{3,}/g, '\n\n')
}

mkdirSync(outDir, { recursive: true })
for (const { file, out, intro } of pages) {
  const schema = JSON.parse(readFileSync(resolve(schemaDir, file), 'utf8'))
  writeFileSync(resolve(outDir, out), render(schema, intro))
  console.log(`wrote docs/${out} from schema/v1/${file}`)
}
