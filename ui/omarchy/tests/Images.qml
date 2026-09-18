import QtQuick
import Quickshell
import "Launcher"
ShellRoot {
  property int stage: 0
  property int notices: 0
  property int textPastes: 0
  Launcher {
    id: app
    cli: Quickshell.env("LAUNCHER_TEST_CLI")
    function notifyDelivery(success, id) { notices++ }
    onPasteTextRequested: textPastes++
  }
  function check(value, reason) { if (!value) throw new Error(reason) }
  Timer {
    interval: 150;repeat:true;running:true
    onTriggered: {
      if(app.busy || app.pasting || app.catalogRefreshing)return
      switch(stage++) {
      case 0:
        app.open("{}")
        app.servers=[{value:"grace",label:"grace"},{value:"workstation",label:"local"}]
        app.host="grace";app.projectId=""
        app.pasteClipboard()
        check(app.pasting && !app.canSend,"send allowed before paste completes")
        break
      case 1:
        check(app.images.length===1 && app.canSend,"image-only prompt cannot send")
        app.inference="missing"
        check(!app.canSend,"image-only prompt bypassed inference validation")
        app.inference=""
        app.changeHost("workstation")
        check(app.images.length===1,"host switch lost attachments")
        app.removeImage(0)
        check(!app.images.length && !app.canSend,"remove left attachment behind")
        app.pasteClipboard()
        break
      case 2:
        app.message="failed";app.send(true)
        app.open("{}")
        check(app.images.length===1,"reopen lost pending image")
        break
      case 3:
        check(app.images.length===1 && app.canSend && !app.uncertain,"known failure lost image or blocked retry")
        app.inferenceModels=[{id:"fixture/model",context_length:32000}];app.inference="fixture/model"
        app.message="images";app.send(true)
        break
      case 4:
        check(!app.images.length && app.message==="" && app.taskId==="image-id","successful delivery kept draft")
        app.open("{}");app.pasteClipboard()
        break
      case 5:
        app.message="uncertain";app.send(true)
        break
      case 6:
        app.open("{}")
        check(app.images.length===1 && app.uncertain && !app.canSend,"uncertain image delivery lost recovery")
        app.removeImage(0)
        check(app.images.length===1,"uncertain attachment was editable")
        app.dismiss()
        check(!app.images.length,"dismiss did not discard draft")
        app.open("{}");app.pasteClipboard();app.dismiss();app.open("{}")
        break
      case 7:
        check(!app.images.length,"late clipboard result contaminated new draft")
        app.cli=Quickshell.env("LAUNCHER_TEST_TEXT_CLI")
        app.pasteClipboard()
        break
      case 8:
        check(textPastes===1 && !app.images.length,"text clipboard did not use normal paste")
        app.dismiss()
        console.log("IMAGE_FIXTURE_PASS");Qt.quit()
      }
    }
  }
}
