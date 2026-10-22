import QtQuick
import QtQuick.Controls.Basic
import Luma

// Enter sends, Shift+Enter adds a line; grows to six lines, then scrolls. Send appears only with text.
Item {
    id: root
    property alias text: area.text
    readonly property bool hasText: area.text.trim().length > 0
    signal submitted(string text)

    function focusInput() { area.forceActiveFocus() }
    function submit() {
        if (!hasText)
            return
        root.submitted(area.text)
        area.clear()
    }

    implicitHeight: field.height + 26

    Rectangle {
        id: field
        anchors { left: parent.left; right: parent.right; leftMargin: 20; rightMargin: 20; verticalCenter: parent.verticalCenter }
        height: Math.min(Math.max(42, area.implicitHeight), 6 * 20 + 22)
        radius: LumaTokens.radius.s
        color: LumaTokens.surfaceSunken
        border.width: area.activeFocus ? 0 : 0

        ScrollView {
            id: scroll
            anchors { left: parent.left; right: send.left; top: parent.top; bottom: parent.bottom; leftMargin: 4; rightMargin: 4 }
            TextArea {
                id: area
                placeholderText: "Message"
                placeholderTextColor: LumaTokens.textTertiary
                color: LumaTokens.text
                selectionColor: LumaTokens.primary
                selectedTextColor: LumaTokens.textOnPrimary
                wrapMode: TextArea.Wrap
                textFormat: TextArea.PlainText
                font.pixelSize: LumaTokens.type.callout.size
                topPadding: 11
                bottomPadding: 11
                verticalAlignment: TextEdit.AlignVCenter
                leftPadding: 10
                background: null
                Accessible.name: "Message"
                Keys.onReturnPressed: (event) => {
                    if (event.modifiers & Qt.ShiftModifier) {
                        event.accepted = false
                    } else {
                        root.submit()
                        event.accepted = true
                    }
                }
                Keys.onEnterPressed: (event) => Keys.returnPressed(event)
                onTextChanged: if (text.length > 0) App.composerEdited()
            }
        }
        AbstractButton {
            id: send
            width: 30
            height: 30
            anchors { right: parent.right; rightMargin: 6; bottom: parent.bottom; bottomMargin: 6 }
            enabled: root.hasText
            opacity: root.hasText ? 1 : 0
            scale: root.hasText ? 1 : 0.85
            Behavior on opacity { NumberAnimation { duration: LumaTokens.duration.quick } }
            Behavior on scale { NumberAnimation { duration: LumaTokens.duration.quick; easing.type: Easing.OutCubic } }
            focusPolicy: Qt.StrongFocus
            Accessible.name: "Send"
            onClicked: root.submit()
            background: Rectangle {
                radius: LumaTokens.radius.xs + 2
                color: LumaTokens.primary
                border.width: send.visualFocus ? 2 : 0
                border.color: LumaTokens.lamp
            }
            contentItem: Item {
                Icon { anchors.centerIn: parent; name: "arrow-up"; size: 17; tint: LumaTokens.textOnPrimary }
            }
        }
    }
}
