import QtQuick
import Luma

// One message. Runs from the same sender tighten their inner corners (no tails); time and the
// luminance receipt sit inside the last bubble of a run (DESIGN.md §4, §6).
Item {
    id: root
    required property int index
    required property string key
    required property string clientId
    required property string body
    required property string senderName
    required property bool mine
    required property string time
    required property string receipt
    required property string groupPos
    required property bool showDate
    required property string dateLabel
    required property bool showSender

    readonly property bool joinedAbove: groupPos === "middle" || groupPos === "last"
    readonly property bool joinedBelow: groupPos === "middle" || groupPos === "first"
    readonly property bool showMeta: groupPos === "last" || groupPos === "single"
    readonly property real r: LumaTokens.radius.bubble
    readonly property real j: LumaTokens.radius.bubbleJoined
    readonly property color metaColor: mine ? LumaTokens.bubbleOutMeta : LumaTokens.bubbleInMeta

    width: ListView.view ? ListView.view.width : 400
    implicitHeight: col.implicitHeight + (joinedBelow ? LumaTokens.space.s1 : LumaTokens.space.s4)
    Accessible.role: Accessible.StaticText
    Accessible.name: (mine ? "You" : senderName) + ", " + time + (receipt ? ", " + receipt : "") + ": " + body

    TextMetrics { id: nbsp; font: bodyText.font; text: " " }

    Column {
        id: col
        width: parent.width
        spacing: 2

        Text {
            textFormat: Text.PlainText
            visible: root.showDate
            text: root.dateLabel
            anchors.horizontalCenter: parent.horizontalCenter
            color: LumaTokens.textTertiary
            font.pixelSize: LumaTokens.type.caption.size
            font.weight: Font.Medium
            topPadding: 6
            bottomPadding: 10
        }
        Text {
            textFormat: Text.PlainText
            visible: root.showSender
            text: root.senderName.split(" ")[0]
            leftPadding: 12
            color: LumaTokens.textSecondary
            font.pixelSize: LumaTokens.type.caption.size
        }
        Item {
            width: parent.width
            height: bubble.height
            Rectangle {
                id: bubble
                readonly property real maxW: Math.min(LumaTokens.size.bubbleMax, root.width * 0.7)
                width: bodyText.width + 24
                height: bodyText.height + 15
                anchors.right: root.mine ? parent.right : undefined
                color: root.mine ? LumaTokens.bubbleOut : LumaTokens.bubbleIn
                topLeftRadius: !root.mine && root.joinedAbove ? root.j : root.r
                bottomLeftRadius: !root.mine && root.joinedBelow ? root.j : root.r
                topRightRadius: root.mine && root.joinedAbove ? root.j : root.r
                bottomRightRadius: root.mine && root.joinedBelow ? root.j : root.r

                Text {
                    id: bodyText
                    // A no-break spacer reserves room for the time on the last line (it wraps with the last word).
                    text: root.body + (root.showMeta ? " " + " ".repeat(Math.ceil((meta.width + 8) / Math.max(1, nbsp.advanceWidth))) : "")
                    textFormat: Text.PlainText // never interpret markup from other people
                    wrapMode: Text.Wrap
                    width: Math.min(implicitWidth, bubble.maxW - 24)
                    x: 12
                    y: 7
                    color: root.mine ? LumaTokens.textOnBubbleOut : LumaTokens.textOnBubbleIn
                    font.pixelSize: LumaTokens.type.callout.size
                    lineHeight: 1.15
                }
                Row {
                    id: meta
                    visible: root.showMeta
                    spacing: 4
                    anchors { right: parent.right; rightMargin: 10; bottom: parent.bottom; bottomMargin: 6 }
                    Text {
                        textFormat: Text.PlainText
                        text: root.time
                        color: root.metaColor
                        font.pixelSize: 11
                        font.weight: Font.Medium
                        font.features: { "tnum": 1 }
                    }
                    Icon {
                        visible: root.mine && root.receipt !== ""
                        name: "receipt-" + root.receipt
                        size: 11
                        tint: root.metaColor
                        anchors.verticalCenter: parent.verticalCenter
                    }
                }
                Behavior on opacity { NumberAnimation { duration: LumaTokens.duration.quick } }
                opacity: root.receipt === "pending" ? 0.72 : 1
            }
        }
        Text {
            textFormat: Text.PlainText
            visible: root.receipt === "failed"
            text: "Couldn’t send. Click to retry."
            color: LumaTokens.danger
            font.pixelSize: LumaTokens.type.caption.size
            anchors.right: parent.right
            rightPadding: 4
            MouseArea {
                anchors.fill: parent
                cursorShape: Qt.PointingHandCursor
                onClicked: App.retry(root.clientId)
            }
            Accessible.role: Accessible.Button
            Accessible.name: "Couldn’t send. Retry."
        }
    }
}
