const {rows}=require('../Inference.js');const assert=require('node:assert/strict');
const models=[{id:'z',name:'Zulu'},{id:'c',name:'Charlie'},{id:'a',name:'Alpha'}];
assert.deepEqual(rows(models,['z'],'').map(m=>m.id),['z','a','c']);
assert.deepEqual(rows(models,['z'],'CHAR').map(m=>m.id),['c']);
assert.equal(rows(models,['missing'],'')[0].unavailable,true);
assert.equal(rows(models,[],'none').length,0);
assert.equal(rows(models,['c','z'],'')[0].heading,'Favorites');
console.log('INFERENCE_MODEL_PASS');
const {parseModelProviders,mappedProvider,providerFor}=require('../Inference.js');
const pins=parseModelProviders('{"deepseek/model":"openrouter_together"}');
assert.equal(providerFor(pins,'deepseek/model'),'openrouter_together');
assert.equal(providerFor(pins,'other/model'),'openrouter');
assert.equal(mappedProvider(pins,'toString'),'');
assert.equal(providerFor(parseModelProviders('{}'),'any'),'openrouter');
for(const raw of ['', 'null', '[]', '"provider"', '{', '{"":"route"}', '{"a b":"route"}', '{"a":null}', '{"a":[]}', '{"a":""}', '{"a":"bad route"}', JSON.stringify({a:'x'.repeat(129)})])
  assert.throws(()=>parseModelProviders(raw),raw);
console.log('MODEL_PROVIDERS_PASS');
