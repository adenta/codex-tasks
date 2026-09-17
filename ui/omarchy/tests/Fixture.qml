import QtQuick
import Quickshell
import "Launcher"
ShellRoot {
 Launcher { id: app; windowEnabled:true }
 Timer { interval: 500; running:true; onTriggered: {
   app.servers=[{value:"grace",label:"grace"}];app.host="grace";app.projectId="";app.message="Test";
   if(!app.canSend)throw new Error("valid prompt cannot send")
   app.busy=true;if(app.canSend)throw new Error("duplicate send possible");app.busy=false;
   app.projectId="missing";if(app.canSend)throw new Error("stale project accepted");
   app.mode="checkout";app.remember();console.log("LAUNCHER_FIXTURE_PASS");Qt.quit()
 } }
}
