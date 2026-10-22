import QtQuick
import Luma

// Initials on a tonal disc. Online people get a luminance halo (DESIGN.md §1), never a colored dot.
Item {
    id: root
    property string initials
    property var pair: []
    property int size: 44
    property bool online: false

    width: size
    height: size
    Accessible.ignored: true

    Rectangle {
        anchors.centerIn: parent
        width: root.size + 6.5
        height: width
        radius: width / 2
        color: "transparent"
        border.width: 1.25
        border.color: LumaTokens.primary
        visible: root.online
    }
    Rectangle {
        anchors.fill: parent
        radius: width / 2
        color: LumaTokens.surfaceSunken
        visible: root.pair.length < 2
        Text {
            anchors.centerIn: parent
            text: root.initials
            color: LumaTokens.textSecondary
            font.pixelSize: Math.round(root.size * 0.36)
            font.weight: Font.DemiBold
        }
    }
    Repeater {
        model: root.pair.length >= 2 ? 2 : 0
        Rectangle {
            required property int index
            width: Math.round(root.size * 0.68)
            height: width
            radius: width / 2
            x: index === 0 ? 0 : root.size - width
            y: index === 0 ? 0 : root.size - height
            color: index === 0 ? LumaTokens.surfaceSunken : LumaTokens.hairline
            border.width: 2
            border.color: LumaTokens.surface
            Text {
                anchors.centerIn: parent
                text: root.pair[parent.index]
                color: LumaTokens.textSecondary
                font.pixelSize: Math.round(root.size * 0.28)
                font.weight: Font.DemiBold
            }
        }
    }
}
