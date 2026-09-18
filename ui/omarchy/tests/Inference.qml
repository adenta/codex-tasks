import QtQuick
import QtTest
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
 Launcher {
  id:state;windowEnabled:false
  servers:[{value:"grace",label:"grace"}];host:"grace"
  inferenceModels:[{id:"a",name:"Very long model name that must be truncated in the selector",context_length:32000,reasoning:{supported_efforts:["max","low"]}}]
 }
 property real originalInferenceWidth: 0
 FloatingWindow {
  visible:true;width:600;height:600
  TestCase { id:keyboard;when:false }
  InferenceDropdown {
   id:picker;x:280;y:20;width:240
   value:"a"
   models:[{id:"a",name:"DeepSeek: DeepSeek V4.1 Flash with a very long model name",context_length:32000},{id:"z",name:"Zulu",context_length:32000}]
   favorites:["z","missing"]
   onChanged:function(id){value=id}
  }
  InferenceRow { id:row;root:state;x:20;y:300;width:552 }
 }
 Timer { interval:300;running:true;onTriggered:{
  var label=findLabel(picker,"inferenceSelectedLabel")
  if(!label||!label.truncated||label.width<=0||label.x+label.width>picker.width)throw new Error("long selected label must fit and elide")
  originalInferenceWidth=findLabel(row,"modelPicker").width
  state.inference="a"
  picker.open()
 } }
 Timer { interval:700;running:true;onTriggered:{
  if(!picker.popupOpen||picker.filtered[0].id!=="missing"||picker.filtered[1].id!=="z")throw new Error("picker failed")
  picker.choose("a");if(picker.value!=="a"||picker.popupOpen)throw new Error("selection failed")
  var model=findLabel(row,"modelPicker"), effort=findLabel(row,"effortPicker"), target=findLabel(row,"targetPicker"), project=findLabel(row,"projectPicker")
  if(model.width!==originalInferenceWidth || Math.abs(target.width-project.width)>0.01 || Math.abs(target.width-effort.width)>0.01 || Math.abs(effort.parent.x+effort.width-row.width)>0.01)throw new Error("effort row geometry")
  if(!effort.enabled || effort.options.length!==3)throw new Error("effort options")
  var selected=findLabel(model,"inferenceSelectedLabel")
  if(!selected.truncated)throw new Error("model label did not elide")
  effort.open()
 } }
 Timer { interval:1000;running:true;onTriggered:{
  var effort=findLabel(row,"effortPicker")
  if(!effort.popupOpen)throw new Error("effort popup failed")
  keyboard.keyClick(Qt.Key_Down)
  keyboard.keyClick(Qt.Key_Down)
  keyboard.keyClick(Qt.Key_Return)
  if(effort.popupOpen)throw new Error("keyboard did not close effort popup")
  if(state.reasoningEffort!=="low")throw new Error("effort selection failed")
  state.inferenceModels=[{id:"a",name:"No effort metadata",context_length:32000}]
  if(state.reasoningEffort!=="" || effort.enabled)throw new Error("unavailable effort retained")
  console.log("INFERENCE_QML_PASS");Qt.quit()
 } }
}
