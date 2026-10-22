import QtQuick
import Luma

// Connection state as a 2px line: invisible when online, breathing while (re)connecting, steady when offline.
Rectangle {
    id: line
    property string conn: App.connection
    readonly property bool breathing: conn === "connecting" || conn === "reconnecting"

    height: LumaTokens.size.lumaLine
    color: LumaTokens.textTertiary
    opacity: conn === "offline" ? 1 : 0

    SequentialAnimation on opacity {
        running: line.breathing
        loops: Animation.Infinite
        NumberAnimation { from: 0.2; to: 1; duration: LumaTokens.duration.lumaPulse; easing.type: Easing.OutCubic }
        NumberAnimation { from: 1; to: 0.2; duration: LumaTokens.duration.lumaPulse; easing.type: Easing.InOutCubic }
    }
    onBreathingChanged: if (!breathing) opacity = conn === "offline" ? 1 : 0
}
