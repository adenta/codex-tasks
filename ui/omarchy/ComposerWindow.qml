import QtQuick
import QtQuick.Controls as QQC
import QtCore
import Quickshell
import Quickshell.Io
import Quickshell.Wayland
import Quickshell.Hyprland
import qs.Commons
import qs.Ui

  PanelWindow {
    required property var root
    function focusEditor() { editor.forceActiveFocus() }
    id: panel
    visible: root.opened
    screen: root.targetScreen
    anchors { top:true;bottom:true;left:true;right:true }
    color:"transparent"
    exclusionMode: ExclusionMode.Ignore
    WlrLayershell.namespace: "codex-tasks-launcher"
    WlrLayershell.layer: WlrLayer.Overlay
    WlrLayershell.keyboardFocus: visible ? WlrKeyboardFocus.Exclusive : WlrKeyboardFocus.None
    Rectangle { anchors.fill:parent;color:Color.menu.scrim }
    MouseArea { anchors.fill:parent;onClicked:root.dismiss() }
    BorderSurface {
      anchors.centerIn:parent
      width:Math.min(Style.space(600),panel.width-Style.space(40))
      height:Math.min(content.implicitHeight+Style.space(48),panel.height-Style.space(40))
      color:Color.popups.background;border.color:Color.popups.border;border.width:1;radius:Style.cornerRadius
      MouseArea { anchors.fill:parent }
      Column {
        id:content
        anchors { left:parent.left;right:parent.right;top:parent.top;margins:Style.space(24) }
        spacing:Style.space(16)
        Row {
          width:parent.width
          Text { width:parent.width-Style.space(36);text:"New remote task";color:Color.popups.text;font.family:Style.font.family;font.pixelSize:Style.space(20);font.bold:true }
          Button { text:"×";width:Style.space(32);enabled:!root.busy;onClicked:root.dismiss() }
        }
        Row {
          width:parent.width;spacing:Style.space(12);enabled:!root.busy && !root.uncertain
          Dropdown { width:parent.width*0.32;label:"Server";value:root.host;options:root.servers;onChanged:function(value){root.changeHost(value)} }
          SearchableDropdown { width:parent.width*0.68-parent.spacing;label:root.refreshing ? "Project · refreshing…" : "Project";value:root.projectId;options:root.projectOptions;onChanged:function(value){root.projectId=value;root.remember();root.status=""} }
        }
        Dropdown {
          width:parent.width;visible:root.selectedProject!==null && root.selectedProject.isGitRepository!==false
          label:"Run in";value:root.mode;options:[{value:"worktree",label:"New worktree"},{value:"checkout",label:"Existing checkout"}]
          enabled:!root.busy && !root.uncertain
          onChanged:function(value){root.mode=value;root.remember()}
        }
        Rectangle {
          width:parent.width;height:Style.space(190);color:Color.menu.background;radius:Style.cornerRadius
          border.color:editor.activeFocus ? Color.accent : Color.popups.border
          QQC.ScrollView {
            anchors.fill:parent;anchors.margins:Style.space(12);clip:true
            QQC.TextArea {
              id:editor
              text:root.message;onTextChanged:root.message=text
              placeholderText:"What would you like Codex to do?"
              placeholderTextColor:Qt.alpha(Color.popups.text,0.5)
              color:Color.popups.text;selectionColor:Color.accent
              font.family:Style.font.family;font.pixelSize:Style.space(16)
              wrapMode:TextEdit.Wrap;enabled:!root.busy && !root.uncertain
              background:null
              Keys.onPressed:function(event){
                if(event.key===Qt.Key_Escape){root.dismiss();event.accepted=true}
                else if((event.key===Qt.Key_Return || event.key===Qt.Key_Enter) && !(event.modifiers & Qt.ShiftModifier) && !inputMethodComposing){root.send();event.accepted=true}
              }
            }
          }
        }
        Row {
          width:parent.width;spacing:Style.space(10);visible:root.status!==""
          QQC.BusyIndicator { width:Style.space(24);height:width;running:root.busy;visible:running }
          Text { width:parent.width-(root.busy ? Style.space(34):0);text:root.status;color:Color.popups.text;font.family:Style.font.family;font.pixelSize:Style.space(13);wrapMode:Text.Wrap }
        }
        Row {
          width:parent.width;spacing:Style.space(10)
          Text { width:parent.width-Style.space(root.taskId?225:100);text:"Enter to send · Shift+Enter for a new line";color:Qt.alpha(Color.popups.text,0.6);font.family:Style.font.family;font.pixelSize:Style.space(12);anchors.verticalCenter:parent.verticalCenter }
          Button { visible:root.taskId!=="";text:"Open task";onClicked:root.openTask() }
          Button { text:root.busy?"Sending…":"Send";enabled:root.canSend;onClicked:root.send() }
        }
      }
      Keys.onEscapePressed:root.dismiss()
    }
  }
