import QtQuick
import Quickshell
import "Launcher"
ShellRoot {
  property int stage: 0
  property int openedTasks: 0
  property int notices: 0
  property bool lastSuccess: false
  Launcher {
    id: app
    windowEnabled: false
    catalogCLI: "/bin/false"
    cli: Quickshell.env("LAUNCHER_TEST_CLI")
    function openTask() { openedTasks++; dismiss() }
    function notifyDelivery(success, id) { notices++; lastSuccess=success }
  }
  function check(value, reason) { if (!value) throw new Error(reason) }
  Timer {
    interval: 100; repeat: true; running: true
    onTriggered: {
      if (app.busy) return
      switch(stage++) {
      case 0:
        app.servers=[{value:"grace",label:"grace"}];app.host="grace";app.projectId=""
        app.opened=true;app.message="background";app.send(true)
        check(!app.opened && app.busy, "background did not immediately hide")
        app.open("{}")
        check(app.message==="background" && app.busy, "reopen lost pending prompt")
        app.send(false)
        app.dismiss()
        check(!app.opened && app.busy, "pending background cannot be hidden")
        break
      case 1:
        check(openedTasks===0 && notices===1 && lastSuccess, "background opened Codex or did not notify")
        check(app.message==="" && app.taskId==="background-id", "background delivery not recorded")
        app.opened=true;app.message="foreground";app.send(false)
        check(app.opened && app.busy, "foreground hid before acceptance")
        break
      case 2:
        check(openedTasks===1 && notices===1 && !app.opened, "foreground did not open exactly once")
        app.message="failed";app.send(true)
        break
      case 3:
        check(notices===2 && !lastSuccess && app.recoveryPending && !app.uncertain, "known failure not retained")
        app.open("{}")
        check(app.message==="failed" && app.canSend, "known failure lost recovery")
        app.message="uncertain";app.send(true)
        break
      case 4:
        check(notices===3 && !lastSuccess && app.recoveryPending && app.uncertain, "unknown delivery not retained")
        app.open("{}")
        check(app.message==="uncertain" && !app.canSend, "uncertain delivery allows duplicate")
        app.send(true)
        check(!app.busy, "uncertain delivery resent")
        app.dismiss()
        console.log("DELIVERY_FIXTURE_PASS");Qt.quit()
      }
    }
  }
}
