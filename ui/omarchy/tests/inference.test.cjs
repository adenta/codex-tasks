const {rows}=require('../Inference.js');const assert=require('node:assert/strict');
const models=[{id:'z',name:'Zulu'},{id:'c',name:'Charlie'},{id:'a',name:'Alpha'}];
assert.deepEqual(rows(models,['z'],'').map(m=>m.id),['z','a','c']);
assert.deepEqual(rows(models,['z'],'CHAR').map(m=>m.id),['c']);
assert.equal(rows(models,['missing'],'')[0].unavailable,true);
assert.equal(rows(models,[],'none').length,0);
assert.equal(rows(models,['c','z'],'')[0].heading,'Favorites');
console.log('INFERENCE_MODEL_PASS');

const {effortOptions}=require('../Inference.js');
assert.deepEqual(effortOptions(null),[{value:'',label:'Default'}]);
assert.deepEqual(effortOptions({reasoning:{supported_efforts:['max','low','low','',null,'bad value']}}).map(o=>o.value),['','max','low']);
