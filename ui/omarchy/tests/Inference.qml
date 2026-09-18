import QtQuick
import QtQuick.Controls as QQC
import Quickshell
import "Launcher"
ShellRoot {
 function findLabel(item,name) {
  if(item.objectName===name)return item
  var children=item.children || []
  for(var i=0;i<children.length;i++){var found=findLabel(children[i],name);if(found)return found}
  return null
 }
 FloatingWindow {
  visible:true;width:600;height:600
  InferenceDropdown {
   id:picker;x:280;y:20;width:240
   value:"a"
   models:[{id:"a",name:"DeepSeek: DeepSeek V4.1 Flash with a very long model name",context_length:32000},{id:"z",name:"Zulu",context_length:32000}]
   favorites:["z","missing"]
   onChanged:function(id){value=id}
  }
 }
 Timer { interval:300;running:true;onTriggered:{
  var label=findLabel(picker,"inferenceSelectedLabel")
  if(!label||!label.truncated||label.width<=0||label.x+label.width>picker.width)throw new Error("long selected label must fit and elide")
  picker.open()
 } }
 Timer { interval:700;running:true;onTriggered:{
  if(!picker.popupOpen||picker.filtered[0].id!=="missing"||picker.filtered[1].id!=="z")throw new Error("picker failed")
  picker.choose("a");if(picker.value!=="a"||picker.popupOpen)throw new Error("selection failed")
  console.log("INFERENCE_QML_PASS");Qt.quit()
 } }
}
