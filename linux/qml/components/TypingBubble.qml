import QtQuick
import Luma

// Three dots that fade in turn; nothing moves vertically (DESIGN.md §5).
Item {
    id: root
    property bool active
    width: parent ? parent.width : 200
    height: active ? 42 : 0
    visible: active
    Accessible.name: "Typing"
    Rectangle {
        y: 4
        width: 54
        height: 34
        radius: LumaTokens.radius.bubble
        color: LumaTokens.bubbleIn
        Row {
            anchors.centerIn: parent
            spacing: 5
            Repeater {
                model: 3
                Rectangle {
                    required property int index
                    width: 7
                    height: 7
                    radius: 3.5
                    color: LumaTokens.textTertiary
                    opacity: 0.3
                    SequentialAnimation on opacity {
                        running: root.active
                        loops: Animation.Infinite
                        PauseAnimation { duration: index * 160 }
                        NumberAnimation { to: 1; duration: 320 }
                        NumberAnimation { to: 0.3; duration: 560 }
                        PauseAnimation { duration: (2 - index) * 160 }
                    }
                }
            }
        }
    }
}
