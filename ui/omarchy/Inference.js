function rows(models, favorites, query) {
  var byId = {}, result = [], needle = String(query || "").toLowerCase().trim()
  ;(models || []).forEach(function(m) { byId[m.id]=m })
  function matches(m) { return (m.name + " " + m.id).toLowerCase().indexOf(needle)>=0 }
  function compare(a,b) { var x=a.name.toLowerCase(),y=b.name.toLowerCase();return x<y?-1:x>y?1:a.id.localeCompare(b.id) }
  var starred=(favorites || []).map(function(id){return byId[id] || {id:id,name:id,unavailable:true}}).filter(matches).sort(compare)
  var rest=(models || []).filter(function(m){return favorites.indexOf(m.id)<0 && matches(m)}).sort(compare)
  starred.forEach(function(m,i){result.push({id:m.id,name:m.name,unavailable:!!m.unavailable,favorite:true,heading:i===0?"Favorites":""})})
  rest.forEach(function(m,i){result.push({id:m.id,name:m.name,unavailable:false,favorite:false,heading:i===0?"All models":""})})
  return result
}
function parseModelProviders(raw) {
  var parsed = JSON.parse(raw)
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("expected a JSON object")
  Object.keys(parsed).forEach(function(id) {
    var provider = parsed[id]
    if (!id || /\s/.test(id) || typeof provider !== "string" || !provider || provider.length > 128 || /\s/.test(provider))
      throw new Error("expected exact model IDs mapped to nonempty provider IDs without whitespace")
  })
  return parsed
}
function mappedProvider(mapping, id) {
  return Object.prototype.hasOwnProperty.call(mapping, id) ? mapping[id] : ""
}
function providerFor(mapping, id) { return mappedProvider(mapping, id) || "openrouter" }
if (typeof module !== "undefined") module.exports={rows:rows,parseModelProviders:parseModelProviders,mappedProvider:mappedProvider,providerFor:providerFor}
