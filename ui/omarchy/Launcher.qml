import QtQuick
import "Inference.js" as Inference
import QtQuick.Controls as QQC
import QtCore
import Quickshell
import Quickshell.Io
import Quickshell.Wayland
import Quickshell.Hyprland
import qs.Commons
import qs.Ui

Item {
  id: root
  property var shell: null
  property var manifest: null
  property bool opened: false
  property bool busy: false
  property bool uncertain: false
  property bool recoveryPending: false
  readonly property bool backgroundSending: busy && createProcess.background
  property string status: ""
  property string taskId: ""
  property string message: ""
  property var images: []
  property int draftGeneration: 0
  property bool pasting: false
  signal pasteTextRequested()
  property var servers: []
  property var desktopProjects: []
  property var cache: ({})
  property string host: settings.host
  property string projectId: ""
  property string inference: ""
  property string reasoningEffort: ""
  onInferenceChanged: reasoningEffort=""
  readonly property var effortOptions: Inference.effortOptions(inferenceModel)
  onEffortOptionsChanged: { if(!effortOptions.some(function(o){return o.value===reasoningEffort}))reasoningEffort="" }
  property var inferenceModels: []
  property var favorites: []
  property string catalogWarning: ""
  property string catalogRefreshedAt: ""
  readonly property bool catalogRefreshing: catalogProcess.running
  readonly property var inferenceModel: inferenceModels.find(function(m){return m.id===root.inference}) || null
  property string mode: settings.mode
  property var targetScreen: null
  property string cli: Quickshell.env("HOME") + "/.local/bin/codex-tasks"
  readonly property var projects: {
    var list=(cache[host] || []).slice()
    desktopProjects.filter(function(p){return p.host===root.host}).forEach(function(p){
      var index=list.findIndex(function(x){return x.roots && x.roots.some(function(r){return r.path===p.path})})
      var entry=index>=0 ? Object.assign({},list[index]) : {roots:[{path:p.path}]}
      entry.serverProjectId=index>=0 ? entry.id : ""
      entry.id="folder:"+p.path;entry.name=p.name || entry.name || p.path;entry.desktopFolder=true
      if(index>=0)list[index]=entry;else list.push(entry)
    })
    return list
  }
  readonly property var selectedProject: projects.find(function(p) { return p.id === root.projectId }) || null
  readonly property bool validServer: servers.some(function(s) { return s.value === root.host })
  readonly property bool canSend: !busy && !pasting && !uncertain && validServer && (!inference || inferenceModel!==null) && (message.trim().length > 0 || images.length > 0) && (projectId === "" || (selectedProject !== null && !selectedProject.unavailable))
  readonly property var projectOptions: {
    var list = [{value:"", label:"No project"}]
    projects.forEach(function(p) { list.push({value:p.id,label:(p.name || p.id) + (p.unavailable ? " (unavailable)" : "")}) })
    if (projectId && !selectedProject) list.push({value:projectId,label:"Previously selected project (unavailable)"})
    return list
  }
  Settings {
    id: settings
    location: "file://" + (Quickshell.env("CODEX_TASKS_LAUNCHER_SETTINGS") || (Quickshell.env("HOME") + "/.config/codex-tasks/launcher.ini"))
    property string host: "grace"
    property string mode: "worktree"
    property string cache: "{}"
    property string favorites: "[]"
  }
  Component.onCompleted: {
    try { cache=JSON.parse(settings.cache) } catch(e) {}
    try { var saved=JSON.parse(settings.favorites);if(Array.isArray(saved))favorites=saved.filter(function(v,i,a){return typeof v==="string"&&a.indexOf(v)===i}) } catch(e) {}
  }
  function remember() {
    settings.host=host; settings.mode=mode
  }
  function open(payload) {
    if (opened) { root.focusEditor(); return }
    opened=true
    if (!busy && !recoveryPending) { status=""; uncertain=false;taskId="";clearDraft();projectId="";inference="";reasoningEffort="" }
    var name=Hyprland.focusedMonitor ? Hyprland.focusedMonitor.name : ""
    targetScreen=Quickshell.screens.find(function(s){return s.name===name}) || Quickshell.screens[0]
    if (!targetsProcess.running) targetsProcess.running=true
    refresh()
    refreshCatalog(false)
    Qt.callLater(function(){root.focusEditor()})
  }
  function close() {
    opened=false
    if (!busy) { clearDraft(); recoveryPending=false }
  }
  function dismiss() {
    if (busy && !createProcess.background) return
    close()
    if(shell && typeof shell.hide === "function") shell.hide("adenta.codex-tasks")
  }
  function toggle() { open("{}") }
  function discardImages(items) {
    cleanupProcess.pending=cleanupProcess.pending.concat(items.map(function(item){return item.path}))
    cleanupProcess.next()
  }
  function clearDraft() {
    draftGeneration++;message="";discardImages(images);images=[]
  }
  function removeImage(index) {
    if(busy || uncertain || pasting)return
    var next=images.slice();discardImages(next.splice(index,1));images=next
  }
  function pasteClipboard() {
    if(busy || uncertain || pasting)return
    pasting=true
    clipboardProcess.generation=draftGeneration
    clipboardProcess.command=[cli,"_clipboard-image"]
    clipboardProcess.running=true
  }
  function attachImage(url) {
    if(busy || uncertain || pasting)return
    if(images.length>=8){status="You can attach up to 8 images.";return}
    pasting=true
    clipboardProcess.generation=draftGeneration
    clipboardProcess.command=[cli,"_import-image",String(url)]
    clipboardProcess.running=true
  }
  function changeHost(value) {
    host=value;projectId="";remember();status="";refresh()
  }
  function refresh() {
    if (busy || !host) return
    if(projectProcess.running) { projectProcess.pendingHost=host;return }
    projectProcess.fetchHost=host;projectProcess.pendingHost="";projectProcess.pages=[];projectProcess.seenCursors=[]
    projectProcess.command=[cli,"projects","--host",host,"--limit","100","--json"]
    projectProcess.running=true
  }
  function toggleFavorite(id) {
    var next=favorites.slice(),i=next.indexOf(id)
    if(i>=0)next.splice(i,1);else next.push(id)
    favorites=next;settings.favorites=JSON.stringify(next)
  }
  function refreshCatalog(force) {
    if(catalogProcess.running)return
    var args=[cli,"models","--json"]
    if(force)args.push("--refresh")
    catalogProcess.command=args
    catalogProcess.running=true
  }
  Process {
    id:catalogProcess
    stdout:StdioCollector { id:catalogOutput }
    onExited:function(code) {
      try {
        var data=JSON.parse(catalogOutput.text)
        if(code!==0||data.error)throw new Error(data.error||"Catalog unavailable")
        if(!Array.isArray(data.models)||!data.models.length)throw new Error("Catalog is empty")
        root.inferenceModels=data.models;root.catalogRefreshedAt=data.refreshed_at||"";root.catalogWarning=data.warning||""
      }catch(e){root.catalogWarning="Could not refresh models. "+(root.inferenceModels.length?"Showing cached models.":"Subscription is available.")}
    }
  }
  function send(background) {
    if (!canSend) return
    var args=[cli,"create","--host",host,"--message-file","-","--json"]
    images.forEach(function(item){args.push("--image",item.path)})
    if (!background) args.push("--wait-history")
    if(inferenceModel)args.push("--model-provider","openrouter","--model",inferenceModel.id,"--model-context-window",String(inferenceModel.context_length))
    if(inferenceModel && reasoningEffort)args.push("--reasoning-effort",reasoningEffort)
    if(projectId) {
      var p=selectedProject
      if(!p.roots || !p.roots.length) { status="Project has no working directory.";return }
      args.push("--cwd",p.roots[0].path)
      if(p.serverProjectId)args.push("--project",p.serverProjectId)
      else if(!p.desktopFolder)args.push("--project",p.id)
      if(mode==="checkout")args.push("--checkout")
    } else args.push("--projectless")
    createProcess.background=!!background;recoveryPending=false
    remember();busy=true;status="Creating task on " + host + "…";taskId=""
    createProcess.command=args;createProcess.prompt=message;createProcess.stdinEnabled=true;createProcess.running=true
    if (background) {
      opened=false
      if(shell && typeof shell.hide === "function") shell.hide("adenta.codex-tasks")
    }
  }
  function openTask() {
    if(!taskId)return
    status="Opening in Codex…"
    if(Qt.openUrlExternally("codex://threads/"+encodeURIComponent(taskId))) { busy=false;dismiss() }
    else { busy=false;status="Task created, but opening Codex failed. Try Open task." }
  }
  function notifyDelivery(success, id) {
    var args=["notify-send","--app-name=Codex Tasks","--expire-time=8000"]
    if (success) args.push("--action=open=Open task", "Task sent", "Codex accepted your task on " + host + ".")
    else args.push("--urgency=critical", "Task delivery needs attention", "Reopen the task launcher to review the result. Your prompt is retained; nothing will be resent automatically.")
    var notification=notificationComponent.createObject(root, {command:args, deliveredTaskId:id})
    notification.running=true
  }
  Component {
    id: notificationComponent
    Process {
      property string deliveredTaskId: ""
      stdout: StdioCollector { id: actionOutput }
      onExited: {
        if(actionOutput.text.trim()==="open" && deliveredTaskId)
          Qt.openUrlExternally("codex://threads/"+encodeURIComponent(deliveredTaskId))
        destroy()
      }
    }
  }
  Process {
    id: targetsProcess
    command: [root.cli,"targets"]
    stdout: StdioCollector { id: targetsOutput }
    stderr: StdioCollector { id: targetsError }
    onExited: function(code) {
      try {
        if(code!==0)throw new Error(targetsError.text || "Could not load servers")
        var data=JSON.parse(targetsOutput.text)
        root.desktopProjects=data.desktop_projects || []
        root.servers=(data.targets || []).map(function(t){return {value:t.host,label:t.local ? "local" : t.host}})
        if(!root.validServer) root.status="Select an available server."
      }catch(e){root.status=String(e)}
    }
  }
  Process {
    id: projectProcess
    property string fetchHost: ""
    property string pendingHost: ""
    property var pages: []
    property var seenCursors: []
    stdout: StdioCollector { id: projectOutput }
    stderr: StdioCollector { id: projectError }
    onExited: function(code) {
      try {
        var data=JSON.parse(projectOutput.text)
        if(code!==0 || data.error)throw new Error(data.error || projectError.text || "Project refresh failed")
        pages=pages.concat(data.projects || [])
        if(data.next_cursor && !pendingHost) {
          if(seenCursors.indexOf(data.next_cursor)>=0)throw new Error("Project pagination did not advance")
          seenCursors=seenCursors.concat([data.next_cursor])
          command=[root.cli,"projects","--host",fetchHost,"--limit","100","--cursor",data.next_cursor,"--json"]
          running=true;return
        }
        var c=Object.assign({},root.cache);c[fetchHost]=pages;root.cache=c;settings.cache=JSON.stringify(c)
        if(fetchHost===root.host && root.projectId && !root.selectedProject)root.status="Selected project is no longer available. Choose a project."
      }catch(e){if(fetchHost===root.host && !root.busy)root.status="Could not refresh " + fetchHost + ": " + String(e)}
      if(pendingHost) { pendingHost="";root.refresh() }
    }
  }
  Process {
    id: createProcess
    property string prompt: ""
    property bool background: false
    onStarted: { write(prompt);prompt="";stdinEnabled=false }
    stdout: StdioCollector { id: createOutput }
    stderr: StdioCollector { id: createError }
    onExited: function(code) {
      root.busy=false
      try {
        var data=JSON.parse(createOutput.text)
        root.taskId=data.task ? data.task.id || "" : ""
        if(code===0 && data.input_accepted && (background || data.history_ready) && root.taskId) {
          if(background) {
            root.clearDraft();root.status="Task sent to " + root.host + "."
            root.notifyDelivery(true, root.taskId)
          } else root.openTask()
          return
        }
        root.uncertain=!!root.taskId || data.outcome==="unknown" || !!data.input_accepted
        root.status=data.error || "Creation could not be confirmed. Inspect tasks before sending again."
        if(root.taskId)root.status+=" Task: "+root.taskId
      }catch(e){root.uncertain=true;root.status="Could not confirm delivery. Check tasks on " + root.host + " before sending again. " + String(createError.text || e)}
      if(background) { root.recoveryPending=true;root.notifyDelivery(false, root.taskId) }
    }
  }
  Process {
    id: clipboardProcess
    property int generation: 0
    stdout: StdioCollector { id: clipboardOutput }
    stderr: StdioCollector { id: clipboardError }
    onExited: function(code) {
      root.pasting=false
      try {
        if(code!==0)throw new Error(clipboardError.text || "Could not read clipboard")
        var data=JSON.parse(clipboardOutput.text)
        if(generation!==root.draftGeneration || !root.opened) {
          if(data.image)root.discardImages([data])
          return
        }
        if(!data.image){root.pasteTextRequested();return}
        if(root.images.length>=8){root.discardImages([data]);root.status="You can attach up to 8 images.";return}
        root.images=root.images.concat([data]);root.status=""
      }catch(e){if(generation===root.draftGeneration && root.opened)root.status=String(e)}
    }
  }
  Process {
    id: cleanupProcess
    property var pending: []
    function next() {
      if(running || !pending.length)return
      command=[root.cli,"_discard-images"].concat(pending);pending=[];running=true
    }
    onExited: Qt.callLater(function(){cleanupProcess.next()})
  }
  property bool windowEnabled: true
  readonly property bool refreshing: projectProcess.running && projectProcess.fetchHost===host
  function focusEditor() { if(windowLoader.item) windowLoader.item.focusEditor() }
  Loader { id:windowLoader; active:root.windowEnabled; Component.onCompleted: { if(active) setSource("ComposerWindow.qml", {root:root}) } }
}
