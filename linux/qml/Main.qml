import QtQuick
import QtQuick.Controls.Basic
import Luma

ApplicationWindow {
    id: window

    required property string forcedTheme

    visible: true
    title: "Luma"
    color: LumaTokens.ground

    Binding {
        target: LumaTokens
        property: "dark"
        value: window.forcedTheme === "dark"
               || (window.forcedTheme === "system" && Application.styleHints.colorScheme === Qt.ColorScheme.Dark)
    }

    Text {
        anchors.centerIn: parent
        text: "Luma"
        color: LumaTokens.text
        font.family: LumaTokens.displayFont
        font.pixelSize: LumaTokens.type.largeTitle.size
        font.weight: LumaTokens.type.largeTitle.weight
        font.letterSpacing: LumaTokens.type.largeTitle.tracking
    }
}
