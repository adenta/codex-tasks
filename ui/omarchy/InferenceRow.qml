import QtQuick
import QtQuick.Controls as QQC
import qs.Commons
import qs.Ui

Row {
  required property var root
  id:selectors
  objectName:"inferenceRow"
  readonly property bool showEffort: root.inferenceModel!==null
  readonly property real inferenceWidth: (width-2*spacing)*0.40
  readonly property real compactWidth: (width-3*spacing-inferenceWidth)/3
  width:parent.width;spacing:Style.space(12);enabled:!root.busy && !root.uncertain
  Dropdown { objectName:"targetPicker";width:selectors.showEffort ? selectors.compactWidth : (parent.width-2*parent.spacing)*0.23;label:"Target";value:root.host;options:root.servers;onChanged:function(value){root.changeHost(value)} }
  SearchableDropdown { objectName:"projectPicker";width:selectors.showEffort ? selectors.compactWidth : (parent.width-2*parent.spacing)*0.37;label:root.refreshing ? (selectors.showEffort ? "Project…" : "Project · refreshing…") : "Project";value:root.projectId;options:root.projectOptions;onChanged:function(value){root.projectId=value;root.status=""} }
  InferenceDropdown { objectName:"modelPicker";width:selectors.inferenceWidth;value:root.inference;defaultLabel:root.defaultLabel;subscriptionAvailable:root.subscriptionModel!=="";models:root.inferenceModels;favorites:root.favorites;refreshing:root.catalogRefreshing;refreshedAt:root.catalogRefreshedAt;warning:root.catalogWarning;onChanged:function(value){root.inference=value;root.status=""};onFavoriteToggled:function(value){root.toggleFavorite(value)};onRefreshRequested:root.refreshCatalog(true) }
  Item {
    visible:selectors.showEffort;width:selectors.compactWidth;height:effortPicker.implicitHeight
    Dropdown { id:effortPicker;objectName:"effortPicker";anchors.fill:parent;label:"Effort";value:root.reasoningEffort;options:root.effortOptions;enabled:options.length>1;onChanged:function(value){root.reasoningEffort=value} }
    HoverHandler { id:effortHover }
    QQC.ToolTip.visible: effortHover.hovered && !effortPicker.enabled
    QQC.ToolTip.text: "Effort selection unavailable: this model has no advertised effort levels. Refresh the model catalog to check for updates."
  }
}
