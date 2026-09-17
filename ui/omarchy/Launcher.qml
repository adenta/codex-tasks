import QtQuick
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
  property string status: ""
  property string taskId: ""
  property string message: ""
  property var servers: []
  property var desktopProjects: []
  property var cache: ({})
  property var projectMemory: ({})
  property string host: settings.host
  property string projectId: ""
  property string mode: settings.mode
  property var targetScreen: null
  readonly property string cli: Quickshell.env("HOME") + "/.local/bin/codex-tasks"
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
  readonly property bool canSend: !busy && !uncertain && validServer && message.trim().length > 0 && (projectId === "" || (selectedProject !== null && !selectedProject.unavailable))
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
    property string projects: "{}"
    property string cache: "{}"
  }
  Component.onCompleted: {
    try { cache=JSON.parse(settings.cache); projectMemory=JSON.parse(settings.projects) } catch(e) {}
    projectId=projectMemory[host] || ""
  }
  function remember() {
    settings.host=host; settings.mode=mode
    var m=Object.assign({},projectMemory);m[host]=projectId;projectMemory=m
    settings.projects=JSON.stringify(m)
  }
  function open(payload) {
    if (opened) { root.focusEditor(); return }
    opened=true; status=""; uncertain=false;taskId="";message=""
    var name=Hyprland.focusedMonitor ? Hyprland.focusedMonitor.name : ""
    targetScreen=Quickshell.screens.find(function(s){return s.name===name}) || Quickshell.screens[0]
    if (!targetsProcess.running) targetsProcess.running=true
    refresh()
    Qt.callLater(function(){root.focusEditor()})
  }
  function close() { if (!busy) { opened=false; message="" } }
  function dismiss() {
    if (busy) return
    close()
    if(shell && typeof shell.hide === "function") shell.hide("adenta.codex-tasks")
  }
  function toggle() { open("{}") }
  function changeHost(value) {
    host=value;projectId=projectMemory[host] || "";remember();status="";refresh()
  }
  function refresh() {
    if (busy || !host) return
    if(projectProcess.running) { projectProcess.pendingHost=host;return }
    projectProcess.fetchHost=host;projectProcess.pendingHost="";projectProcess.pages=[];projectProcess.seenCursors=[]
    projectProcess.command=[cli,"projects","--host",host,"--limit","100","--json"]
    projectProcess.running=true
  }
  function send() {
    if (!canSend) return
    var args=[cli,"create","--host",host,"--wait-history","--message-file","-","--json"]
    if(projectId) {
      var p=selectedProject
      if(!p.roots || !p.roots.length) { status="Project has no working directory.";return }
      args.push("--cwd",p.roots[0].path)
      if(p.serverProjectId)args.push("--project",p.serverProjectId)
      else if(!p.desktopFolder)args.push("--project",p.id)
      if(mode==="checkout")args.push("--checkout")
    } else args.push("--projectless")
    remember();busy=true;status="Creating task on " + host + "…";taskId=""
    createProcess.command=args;createProcess.prompt=message;createProcess.stdinEnabled=true;createProcess.running=true
  }
  function openTask() {
    if(!taskId)return
    status="Opening in Codex…"
    if(Qt.openUrlExternally("codex://threads/"+encodeURIComponent(taskId))) { busy=false;dismiss() }
    else { busy=false;status="Task created, but opening Codex failed. Try Open task." }
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
        root.servers=(data.targets || []).filter(function(t){return !t.native_only}).map(function(t){return {value:t.host,label:t.host}})
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
    onStarted: { write(prompt);prompt="";stdinEnabled=false }
    stdout: StdioCollector { id: createOutput }
    stderr: StdioCollector { id: createError }
    onExited: function(code) {
      root.busy=false
      try {
        var data=JSON.parse(createOutput.text)
        root.taskId=data.task ? data.task.id || "" : ""
        if(code===0 && data.input_accepted && data.history_ready && root.taskId) {root.openTask();return}
        root.uncertain=!!root.taskId || data.outcome==="unknown" || !!data.input_accepted
        root.status=data.error || "Creation could not be confirmed. Inspect tasks before sending again."
        if(root.taskId)root.status+=" Task: "+root.taskId
      }catch(e){root.uncertain=true;root.status="Could not confirm delivery. Check tasks on " + root.host + " before sending again. " + String(createError.text || e)}
    }
  }
  property bool windowEnabled: true
  readonly property bool refreshing: projectProcess.running && projectProcess.fetchHost===host
  function focusEditor() { if(windowLoader.item) windowLoader.item.focusEditor() }
  Loader { id:windowLoader; active:root.windowEnabled; Component.onCompleted: { if(active) setSource("ComposerWindow.qml", {root:root}) } }
}
