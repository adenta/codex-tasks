import QtQuick
import Quickshell
import "Launcher"
ShellRoot {
 Launcher { id: app; windowEnabled:false }
 Timer { interval: 500; running:true; onTriggered: {
   app.servers=[{value:"grace",label:"grace"}];app.host="grace";app.projectId="";app.message="Test";
   if(!app.canSend)throw new Error("valid prompt cannot send")
   app.busy=true;if(app.canSend)throw new Error("duplicate send possible");app.busy=false;
   app.projectId="missing";if(app.canSend)throw new Error("stale project accepted");
   app.desktopProjects=[{host:"grace",path:"/projects/test",name:"Test"}];app.projectId="folder:/projects/test";
   if(!app.canSend)throw new Error("desktop folder unavailable")
   app.cache={grace:[{id:"server-id",roots:[{path:"/projects/test"}],isGitRepository:true}]};
   if(!app.canSend || app.selectedProject.serverProjectId!=="server-id")throw new Error("folder selection changed after registration")
   app.mode="checkout";app.remember();console.log("LAUNCHER_FIXTURE_PASS");Qt.quit()
 } }
}
