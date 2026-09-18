import QtQuick
import Quickshell
import "Launcher"
ShellRoot {
  property int stage: 0
  Launcher {
    id: app
    windowEnabled: false
    cli: Quickshell.env("LAUNCHER_TEST_CLI")
    function notifyDelivery(success,id) {}
  }
  function check(value,reason) { if(!value)throw new Error(reason) }
  function project(path) {
    app.desktopProjects=[{host:app.host,path:path,name:path}]
    app.projectId="folder:"+path
  }
  Timer {
    interval:100;repeat:true;running:true
    onTriggered: {
      if(app.busy || app.environmentsLoading)return
      switch(stage++) {
      case 0:
        app.servers=[{value:"grace",label:"grace"}];app.host="grace";app.mode="worktree";app.opened=true;app.message="environment"
        project("/projects/one")
        check(!app.canSend,"sent before discovery")
        break
      case 1:
        check(app.environment==="environment.toml" && app.canSend,"sole environment not selected")
        app.chooseEnvironment("");project("/projects/multiple")
        break
      case 2:
        check(app.environment==="@choose" && !app.canSend,"multiple environments silently selected")
        app.chooseEnvironment("two.toml");check(app.canSend,"explicit choice rejected")
        project("/projects/one")
        break
      case 3:
        check(app.environment==="" && app.canSend,"explicit None not remembered")
        app.chooseEnvironment("environment.toml")
        project("/projects/slow");project("/projects/one")
        break
      case 4:
        check(app.environment==="environment.toml" && app.environments[0].id!=="stale.toml","stale response used")
        app.chooseEnvironment("missing.toml");check(!app.canSend,"missing remembered choice accepted")
        app.mode="checkout";check(app.canSend && !app.usesNewWorktree,"checkout requires setup")
        app.mode="worktree"
        break
      case 5:
        check(app.environment==="missing.toml" && !app.canSend,"missing selection silently replaced")
        app.chooseEnvironment("environment.toml");app.send(true)
        break
      case 6:
        check(app.taskId==="environment-id" && !app.uncertain,"environment flag not delivered")
        app.opened=true;app.message="setup-failed";app.send(true)
        break
      case 7:
        check(app.uncertain && app.recoveryPending && !app.canSend,"failed setup can be resent")
        check(app.status.indexOf("/tmp/retained-fixture")>=0,"worktree recovery path missing")
        app.uncertain=false;app.busy=false;app.message="test";project("/projects/error")
        break
      case 8:
        check(!!app.environmentError && !app.canSend,"discovery failure ignored")
        project("/projects/plain")
        break
      case 9:
        check(!app.usesNewWorktree && app.canSend,"non-Git project blocked")
        app.projectId="";check(app.canSend,"projectless task blocked")
        console.log("ENVIRONMENT_FIXTURE_PASS");Qt.quit()
      }
    }
  }
}
