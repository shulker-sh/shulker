import { readFileSync, writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const changelog = readFileSync(resolve(root, '..', 'CHANGELOG.md'), 'utf8')

writeFileSync(
  resolve(root, 'docs', 'changelog.md'),
  [
    '---',
    `description: ${JSON.stringify('What changed in each shulker release.')}`,
    'editLink: false',
    '---',
    '',
    changelog,
  ].join('\n'),
)
