import fs from 'node:fs';
import vm from 'node:vm';

const sourcePath = process.argv[2] ?? '../pi/packages/ai/src/models.generated.ts';
const outputPath = process.argv[3] ?? 'packages/ai/src/models_generated.go';
const input = fs.readFileSync(sourcePath, 'utf8');
let code = input
  .replace(/^\s*\/\/.*$/gm, '')
  .replace(/import type[^;]+;\s*/g, '')
  .replace(/export const MODELS\s*=\s*/, 'const MODELS = ')
  .replace(/\s+satisfies\s+Model<[^>]+>/g, '')
  .replace(/\s+as const satisfies Record<string, Record<string, Model<Api>>>;?/, ';');
code += '\nMODELS;';
const models = vm.runInNewContext(code, {});
const json = JSON.stringify(models).replace(/`/g, '`+"`"+`');
const providerOrder = Object.keys(models);
const idOrder = Object.fromEntries(Object.entries(models).map(([provider, providerModels]) => [provider, Object.keys(providerModels)]));
const providerOrderGo = providerOrder.map((provider) => `\t${JSON.stringify(provider)},`).join('\n');
const idOrderJSON = JSON.stringify(idOrder).replace(/`/g, '`+"`"+`');
const out = `package ai\n\nimport \"encoding/json\"\n\nvar modelsJSON = \`${json}\`\n\nvar modelProviderOrder = []Provider{\n${providerOrderGo}\n}\n\nvar modelIDOrderJSON = \`${idOrderJSON}\`\n\nvar Models = mustLoadModels()\nvar modelIDOrder = mustLoadModelIDOrder()\n\nfunc mustLoadModels() map[Provider]map[string]Model {\n\tvar raw map[Provider]map[string]Model\n\tif err := json.Unmarshal([]byte(modelsJSON), &raw); err != nil {\n\t\tpanic(err)\n\t}\n\treturn raw\n}\n\nfunc mustLoadModelIDOrder() map[Provider][]string {\n\tvar raw map[Provider][]string\n\tif err := json.Unmarshal([]byte(modelIDOrderJSON), &raw); err != nil {\n\t\tpanic(err)\n\t}\n\treturn raw\n}\n`;
fs.writeFileSync(outputPath, out);
