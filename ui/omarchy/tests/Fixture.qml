import QtQuick
import QtCore
import Quickshell
import "Launcher"
ShellRoot {
 Settings { id:writer;location:"file://"+Quickshell.env("CODEX_TASKS_LAUNCHER_SETTINGS") }
 Launcher { id: app; windowEnabled:false;cli:"/bin/false" }
 Timer { interval: 500; running:true; onTriggered: {
   app.servers=[{value:"grace",label:"grace"}];app.host="grace";app.projectId="";app.message="Test";
   if(!app.canSend)throw new Error("valid prompt cannot send")
   app.busy=true;if(app.canSend)throw new Error("duplicate send possible");app.busy=false;
   app.projectId="missing";if(app.canSend)throw new Error("stale project accepted");
   app.desktopProjects=[{host:"grace",path:"/projects/test",name:"Test"}];app.projectId="folder:/projects/test";
   if(!app.canSend)throw new Error("desktop folder unavailable")
   app.cache={grace:[{id:"server-id",roots:[{path:"/projects/test"}],isGitRepository:true}]};
   if(!app.canSend || app.selectedProject.serverProjectId!=="server-id")throw new Error("folder selection changed after registration")
   writer.setValue("modelProviders",'{"fixture/model":"openrouter_together"}');writer.sync();app.loadModelProviders()
   if(app.modelProviders["fixture/model"]!=="openrouter_together" || app.modelProvidersError)throw new Error("mapping not loaded")
   app.mode="checkout";app.remember();app.toggleFavorite("fixture/model");app.loadModelProviders()
   if(app.modelProviders["fixture/model"]!=="openrouter_together")throw new Error("settings save lost mapping")
   writer.setValue("modelProviders",'[]');writer.sync();app.loadModelProviders()
   app.inferenceModels=[{id:"fixture/model",context_length:32000}];app.inference="fixture/model"
   if(!app.modelProvidersError || app.canSend)throw new Error("invalid mapping accepted")
   app.inference="";if(!app.canSend)throw new Error("invalid mapping blocked Subscription")
   writer.setValue("modelProviders",'{"fixture/model":"openrouter_together"}');writer.sync();app.open("{}")
   if(app.modelProvidersError || app.modelProviders["fixture/model"]!=="openrouter_together")throw new Error("open did not reload corrected mapping")
   console.log("LAUNCHER_FIXTURE_PASS");Qt.quit()
 } }
}
