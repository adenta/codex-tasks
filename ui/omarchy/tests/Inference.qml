import QtQuick
import QtQuick.Controls as QQC
import Quickshell
import "Launcher"
ShellRoot {
 property string toggledFavorite:""
 function findLabel(item,name,seen) {
  seen=seen || []
  if(!item || seen.indexOf(item)>=0)return null
  seen.push(item)
  if(item.objectName===name)return item
  var children=[], lists=[item.children || [],item.data || [],item.contentItem ? [item.contentItem] : []]
  for(var j=0;j<lists.length;j++)for(var k=0;k<lists[j].length;k++)children.push(lists[j][k])
  for(var i=0;i<children.length;i++){var found=findLabel(children[i],name,seen);if(found)return found}
  return null
 }
 FloatingWindow {
  id:testWindow
  visible:true;width:600;height:600
  InferenceDropdown {
   id:picker;x:280;y:20;width:240
   value:"a"
   models:[{id:"a",name:"DeepSeek: DeepSeek V4.1 Flash with a very long model name",context_length:32000},{id:"z",name:"Zulu",context_length:32000}]
   favorites:["z","missing"]
   modelProviders:({a:"openrouter_together"})
   onChanged:function(id){value=id}
   onFavoriteToggled:function(id){toggledFavorite=id}
  }
 }
 Timer { interval:300;running:true;onTriggered:{
  var label=findLabel(picker,"inferenceSelectedLabel")
  if(!label||!label.truncated||label.width<=0||label.x+label.width>picker.width)throw new Error("long selected label must fit and elide")
  picker.open()
 } }
 Timer { interval:700;running:true;onTriggered:{
  if(!picker.popupOpen||picker.filtered[0].id!=="missing"||picker.filtered[1].id!=="z")throw new Error("picker failed")
  var routed=findLabel(testWindow.contentItem,"inferenceRoute_a"),unmapped=findLabel(testWindow.contentItem,"inferenceRoute_z")
  var star=findLabel(testWindow.contentItem,"inferenceStar_a"),otherStar=findLabel(testWindow.contentItem,"inferenceStar_z")
  if(!routed||!routed.visible||routed.Accessible.description!=="Configured provider: openrouter_together")throw new Error("routing indicator missing")
  if(!unmapped||unmapped.visible||star.x!==otherStar.x||routed.parent.x<star.x+star.width)throw new Error("routing column incorrect")
  if(star.parent.children[0].tooltipText.indexOf("Configured provider: openrouter_together")<0)throw new Error("row tooltip missing route")
  var rowLabel=findLabel(star.parent.children[0],"inferenceModelLabel")
  if(!rowLabel || !rowLabel.truncated || rowLabel.width<=0)throw new Error("routing icon broke label elision")
  star.clicked()
  if(toggledFavorite!=="a" || !picker.popupOpen || picker.value!=="a")throw new Error("star changed selection")
  picker.choose("a");if(picker.value!=="a"||picker.popupOpen)throw new Error("selection failed")
  console.log("INFERENCE_QML_PASS");Qt.quit()
 } }
}
