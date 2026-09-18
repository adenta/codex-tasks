import QtQuick
import QtQuick.Controls as QQC
import QtQuick.Dialogs
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
    FileDialog {
      id:imagePicker
      title:"Attach image"
      fileMode:FileDialog.OpenFile
      nameFilters:["Images (*.png *.jpg *.jpeg)"]
      onAccepted:{root.attachImage(selectedFile);Qt.callLater(panel.focusEditor)}
      onRejected:Qt.callLater(panel.focusEditor)
    }
    screen: root.targetScreen
    anchors { top:true;bottom:true;left:true;right:true }
    color:"transparent"
    exclusionMode: ExclusionMode.Ignore
    WlrLayershell.namespace: "codex-tasks-launcher"
    // Let the ordinary desktop file dialog receive input above this layer surface.
    WlrLayershell.layer: imagePicker.visible ? WlrLayer.Bottom : WlrLayer.Overlay
    WlrLayershell.keyboardFocus: visible && !imagePicker.visible ? WlrKeyboardFocus.Exclusive : WlrKeyboardFocus.None
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
          Text { width:parent.width-closeButton.width;text:"New task";color:Color.popups.text;font.family:Style.font.family;font.pixelSize:Style.space(20);font.bold:true;anchors.verticalCenter:parent.verticalCenter }
          PanelActionButton {
            id:closeButton
            iconText:"×"
            bordered:true;focusable:true
            tooltipText:"Close (Esc)"
            enabled:!root.busy || root.backgroundSending
            onClicked:root.dismiss()
          }
        }
        Row {
          width:parent.width;spacing:Style.space(12);enabled:!root.busy && !root.uncertain
          Dropdown { width:(parent.width-2*parent.spacing)*0.23;label:"Target";value:root.host;options:root.servers;onChanged:function(value){root.changeHost(value)} }
          SearchableDropdown { width:(parent.width-2*parent.spacing)*0.37;label:root.refreshing ? "Project · refreshing…" : "Project";value:root.projectId;options:root.projectOptions;onChanged:function(value){root.projectId=value;root.remember();root.status=""} }
          InferenceDropdown { width:(parent.width-2*parent.spacing)*0.40;value:root.inference;models:root.inferenceModels;favorites:root.favorites;refreshing:root.catalogRefreshing;refreshedAt:root.catalogRefreshedAt;warning:root.catalogWarning;onChanged:function(value){root.inference=value;root.status=""};onFavoriteToggled:function(value){root.toggleFavorite(value)};onRefreshRequested:root.refreshCatalog(true) }
        }
        Dropdown {
          width:parent.width;visible:root.selectedProject!==null && root.selectedProject.isGitRepository!==false
          label:"Run in";value:root.mode;options:[{value:"worktree",label:"New worktree"},{value:"checkout",label:"Existing checkout"}]
          enabled:!root.busy && !root.uncertain
          onChanged:function(value){root.mode=value;root.remember()}
        }
        Column {
          width:parent.width;spacing:Style.space(6);visible:root.usesNewWorktree
          Dropdown {
            width:parent.width;label:root.environmentsLoading ? "Environment · loading…" : "Environment"
            value:root.environment;options:root.environmentOptions
            enabled:!root.busy && !root.uncertain && !root.environmentsLoading && !root.environmentError
            onChanged:function(value){root.chooseEnvironment(value)}
          }
          Text {
            width:parent.width;visible:text!==""
            text:root.environmentError || (root.selectedEnvironment ? root.selectedEnvironment.error || "" : "")
            color:Color.popups.text;font.family:Style.font.family;font.pixelSize:Style.space(12);wrapMode:Text.Wrap
          }
          Button {
            text:"Retry environment discovery";visible:root.environmentError!==""
            enabled:!root.busy && !root.uncertain && !root.environmentsLoading
            onClicked:root.refreshEnvironments()
          }
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
              Connections { target:root;function onPasteTextRequested(){editor.paste()} }
              Keys.onPressed:function(event){
                if(event.matches(StandardKey.Paste)){root.pasteClipboard();event.accepted=true}
                else if(event.key===Qt.Key_Escape){root.dismiss();event.accepted=true}
                else if((event.key===Qt.Key_Return || event.key===Qt.Key_Enter) && !(event.modifiers & Qt.ShiftModifier) && !inputMethodComposing){root.send(!!(event.modifiers & Qt.AltModifier));event.accepted=true}
              }
            }
          }
        }
        Button {
          text:"Attach image…";bordered:true;focusable:true
          enabled:!root.busy && !root.uncertain && !root.pasting && root.images.length<8
          onClicked:imagePicker.open()
        }
        Flow {
          width:parent.width;spacing:Style.space(8);visible:root.images.length>0
          Repeater {
            model:root.images
            Rectangle {
              required property var modelData
              required property int index
              width:Style.space(90);height:Style.space(80);radius:Style.cornerRadius
              color:Color.menu.background;border.color:Color.popups.border;border.width:1
              Image { anchors.fill:parent;anchors.margins:Style.space(5);source:modelData.url;fillMode:Image.PreserveAspectFit;sourceSize.width:180;sourceSize.height:160 }
              Button { anchors.right:parent.right;anchors.top:parent.top;text:"×";width:Style.space(24);height:width;tooltipText:"Remove image";enabled:!root.busy && !root.uncertain && !root.pasting;onClicked:root.removeImage(index) }
            }
          }
        }
        Row {
          width:parent.width;spacing:Style.space(10);visible:root.status!==""
          QQC.BusyIndicator { width:Style.space(24);height:width;running:root.busy;visible:running }
          QQC.ScrollView {
            id:statusScroll
            width:parent.width-(root.busy ? Style.space(34):0)
            height:Math.min(statusText.implicitHeight,Style.space(120));clip:true
            contentWidth:availableWidth
            Text { id:statusText;width:statusScroll.availableWidth;text:root.status;textFormat:Text.PlainText;color:Color.popups.text;font.family:Style.font.family;font.pixelSize:Style.space(13);wrapMode:Text.Wrap }
          }
        }
        Text { width:parent.width;text:"Enter to send & open · Alt+Enter to send in background\nShift+Enter for a new line · Ctrl+V to paste text or images";color:Qt.alpha(Color.popups.text,0.6);font.family:Style.font.family;font.pixelSize:Style.space(12);wrapMode:Text.Wrap }
        Flow {
          width:parent.width;spacing:Style.space(10)
          Button {
            text:"Send in background";height:Style.space(40)
            bordered:true;focusable:true;enabled:root.canSend
            tooltipText:"Alt+Enter · Close immediately without opening Codex"
            onClicked:root.send(true)
          }
          Button { id:openButton;visible:root.taskId!=="";text:"Open task";bordered:true;focusable:true;enabled:!root.busy;onClicked:root.openTask() }
          Button {
            id:sendButton
            text:root.busy?"Sending…":"Send & open"
            width:Style.space(150);height:Style.space(40)
            bordered:true;focusable:true
            foreground:Color.accent
            background:Qt.alpha(Color.accent,enabled?0.18:0.08)
            opacity:enabled?1:0.55
            enabled:root.canSend;onClicked:root.send(false)
          }
        }
      }
      Keys.onEscapePressed:root.dismiss()
    }
  }
