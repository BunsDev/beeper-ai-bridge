import fs from 'node:fs';
import vm from 'node:vm';

const sourcePath = process.argv[2] ?? '../pi/packages/ai/src/image-models.generated.ts';
const outputPath = process.argv[3] ?? 'packages/ai/src/image_models_generated.go';
const input = fs.readFileSync(sourcePath, 'utf8');
let code = input
  .replace(/^\s*\/\/.*$/gm, '')
  .replace(/import type[^;]+;\s*/g, '')
  .replace(/export const IMAGE_MODELS\s*=\s*/, 'const IMAGE_MODELS = ')
  .replace(/\s+satisfies\s+ImagesModel<[^>]+>/g, '')
  .replace(/\s+as const satisfies Record<string, Record<string, ImagesModel<ImagesApi>>>;?/, ';');
code += '\nIMAGE_MODELS;';
const models = vm.runInNewContext(code, {});
const json = JSON.stringify(models).replace(/`/g, '`+"`"+`');
const providerOrder = Object.keys(models);
const idOrder = Object.fromEntries(Object.entries(models).map(([provider, providerModels]) => [provider, Object.keys(providerModels)]));
const providerOrderGo = providerOrder.map((provider) => `\t${JSON.stringify(provider)},`).join('\n');
const idOrderJSON = JSON.stringify(idOrder).replace(/`/g, '`+"`"+`');
const out = `package ai\n\nimport \"encoding/json\"\n\nvar imageModelsJSON = \`${json}\`\n\nvar imageModelProviderOrder = []ImagesProvider{\n${providerOrderGo}\n}\n\nvar imageModelIDOrderJSON = \`${idOrderJSON}\`\n\nvar ImageModels = mustLoadImageModels()\nvar imageModelIDOrder = mustLoadImageModelIDOrder()\n\nfunc mustLoadImageModels() map[ImagesProvider]map[string]ImagesModel {\n\tvar raw map[ImagesProvider]map[string]ImagesModel\n\tif err := json.Unmarshal([]byte(imageModelsJSON), &raw); err != nil {\n\t\tpanic(err)\n\t}\n\treturn raw\n}\n\nfunc mustLoadImageModelIDOrder() map[ImagesProvider][]string {\n\tvar raw map[ImagesProvider][]string\n\tif err := json.Unmarshal([]byte(imageModelIDOrderJSON), &raw); err != nil {\n\t\tpanic(err)\n\t}\n\treturn raw\n}\n`;
fs.writeFileSync(outputPath, out);
