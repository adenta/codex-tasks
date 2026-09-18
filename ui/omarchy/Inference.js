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
function effortOptions(model) {
  var result=[{value:"",label:"Default"}], seen={}
  var efforts=model && model.reasoning && model.reasoning.supported_efforts
  if(Array.isArray(efforts))efforts.forEach(function(e){
    if(typeof e!=="string" || !e || e.length>64 || /\s/.test(e) || e==="default" || seen[e])return
    seen[e]=true;result.push({value:e,label:e.charAt(0).toUpperCase()+e.slice(1)})
  })
  return result
}
if (typeof module !== "undefined") module.exports={rows:rows,effortOptions:effortOptions}
