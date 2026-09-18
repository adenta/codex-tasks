import QtQuick
import QtQuick.Controls as QQC
import qs.Commons
import qs.Ui
import "Inference.js" as Inference

Item {
  id: control
  property string value: ""
  property var models: []
  property var favorites: []
  property bool refreshing: false
  property string refreshedAt: ""
  property string warning: ""
  signal changed(string value)
  signal favoriteToggled(string value)
  signal refreshRequested()
  readonly property var selected: models.find(function(m){return m.id===control.value}) || null
  readonly property var filtered: Inference.rows(models,favorites,search.text)
  readonly property bool popupOpen: popup.opened
  implicitHeight: layout.implicitHeight
  function open() { popup.open() }
  function choose(id) { changed(id);popup.close();trigger.forceActiveFocus() }
  Column {
    id:layout;width:parent.width;spacing:Style.spacing.labelGap
    Text { text:"Inference";color:Color.popups.text;font.family:Style.font.family;font.pixelSize:Style.font.caption;font.bold:true }
    Button {
      id:trigger;width:parent.width;height:Style.spacing.controlHeight
      text:control.value ? (control.selected ? control.selected.name : "Unavailable model") : "Subscription"
      iconText:"󰅀";bordered:true;focusable:true;leftAlign:true;foreground:Color.popups.text
      onClicked:popup.opened?popup.close():popup.open()
      Keys.onDownPressed:popup.open()
      QQC.Popup {
        id:popup
        x:trigger.width-width;y:trigger.height+Style.spacing.xxs
        width:Math.max(trigger.width,Math.min(Style.space(340),control.Window.window ? control.Window.window.width-Style.space(60) : Style.space(340)))
        padding:Style.space(8);focus:true
        closePolicy:QQC.Popup.CloseOnEscape | QQC.Popup.CloseOnPressOutside
        background:BorderSurface { color:Color.popups.background;border.color:Color.popups.border;border.width:1;radius:Style.cornerRadius }
        onOpened: { search.text="";results.currentIndex=-1;Qt.callLater(function(){search.forceActiveFocus()}) }
        contentItem:Column {
          spacing:Style.space(6)
          Button {
            id:subscription;width:parent.width;leftAlign:true;foreground:Color.popups.text;text:"Subscription"+(control.value===""?"   ✓":"");focusable:true
            tooltipText:"Use your current Codex subscription model"
            onClicked:control.choose("")
          }
          Text { text:"Use your current Codex model";color:Qt.alpha(Color.popups.text,0.65);font.family:Style.font.family;font.pixelSize:Style.font.caption;leftPadding:Style.space(10) }
          Rectangle { width:parent.width;height:1;color:Qt.alpha(Color.popups.text,0.25) }
          Row {
            width:parent.width;spacing:Style.space(4)
            TextField {
              id:search;width:parent.width-refreshButton.width-parent.spacing
              placeholderText:"Search models…";font.family:Style.font.family;font.pixelSize:Style.font.body
              onTextChanged:results.currentIndex=-1
              Keys.onDownPressed: { if(results.count){results.currentIndex=0;results.forceActiveFocus()} }
              Keys.onUpPressed:subscription.forceActiveFocus()
              Keys.onReturnPressed: { if(results.count&&!control.filtered[0].unavailable)control.choose(control.filtered[0].id) }
              Keys.onEnterPressed: { if(results.count&&!control.filtered[0].unavailable)control.choose(control.filtered[0].id) }
            }
            Button { id:refreshButton;width:Style.space(32);height:Style.spacing.controlHeight;text:"↻";focusable:true;enabled:!control.refreshing;tooltipText:"Refresh models";onClicked:control.refreshRequested() }
          }
          ListView {
            id:results;width:parent.width;height:Math.min(contentHeight,Style.space(240));clip:true
            model:control.filtered;currentIndex:-1;keyNavigationEnabled:false
            boundsBehavior:Flickable.StopAtBounds
            QQC.ScrollBar.vertical:QQC.ScrollBar {}
            function selectCurrent() { if(currentIndex>=0&&currentIndex<count&&!control.filtered[currentIndex].unavailable)control.choose(control.filtered[currentIndex].id) }
            Keys.onPressed:function(event){
              if(event.key===Qt.Key_Down){currentIndex=Math.min(count-1,currentIndex+1);event.accepted=true}
              else if(event.key===Qt.Key_Up){if(currentIndex<=0)search.forceActiveFocus();else currentIndex--;event.accepted=true}
              else if(event.key===Qt.Key_Return||event.key===Qt.Key_Enter){selectCurrent();event.accepted=true}
              else if(event.key===Qt.Key_Space && currentIndex>=0){control.favoriteToggled(control.filtered[currentIndex].id);event.accepted=true}
              if(currentIndex>=0)positionViewAtIndex(currentIndex,ListView.Contain)
            }
            delegate:Column {
              required property var modelData
              required property int index
              width:results.width
              Text { visible:modelData.heading!=="";text:modelData.heading;topPadding:Style.space(6);bottomPadding:Style.space(4);leftPadding:Style.space(8);color:Qt.alpha(Color.popups.text,0.65);font.family:Style.font.family;font.pixelSize:Style.font.caption }
              Row {
                width:parent.width
                Button {
                  width:parent.width-star.width;height:Style.space(34);text:modelData.name+(modelData.unavailable?" (unavailable)":control.value===modelData.id?"  ✓":"")
                  foreground:Color.popups.text;background:results.currentIndex===index?Qt.alpha(Color.accent,0.15):"transparent"
                  leftAlign:true;enabled:!modelData.unavailable;tooltipText:modelData.id;focusable:true
                  onClicked:control.choose(modelData.id)
                }
                Button { id:star;width:Style.space(34);height:width;text:modelData.favorite?"★":"☆";foreground:modelData.favorite?Color.accent:Color.popups.text;focusable:true;tooltipText:modelData.favorite?"Remove favorite":"Add favorite";onClicked:control.favoriteToggled(modelData.id) }
              }
            }
          }
          Text { visible:results.count===0;text:control.refreshing?"Loading models…":"No matching models";width:parent.width;wrapMode:Text.Wrap;color:Color.popups.text;font.family:Style.font.family;font.pixelSize:Style.font.caption }
          Rectangle { width:parent.width;height:1;color:Qt.alpha(Color.popups.text,0.25) }
          Text { width:parent.width;text:control.refreshing?"Refreshing…":control.warning || (control.refreshedAt?"Updated "+new Date(control.refreshedAt).toLocaleString():"Refresh to load models");wrapMode:Text.Wrap;color:Qt.alpha(Color.popups.text,0.65);font.family:Style.font.family;font.pixelSize:Style.font.caption }
        }
      }
    }
  }
}
