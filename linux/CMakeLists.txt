cmake_minimum_required(VERSION 3.25)
project(Luma VERSION 0.1.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

find_package(Qt6 6.5 REQUIRED COMPONENTS Core Gui Quick QuickControls2 Network WebSockets Sql Svg Test)
find_package(Qt6Keychain REQUIRED)
qt_standard_project_setup(REQUIRES 6.5)

set(LUMA_DESIGN_DIR ${CMAKE_CURRENT_SOURCE_DIR}/../design/generated)

# ── Core: protocol, storage and sync; no QML, unit-testable ────────────────────
qt_add_library(luma_core STATIC)
target_sources(luma_core PRIVATE
    src/IconProvider.cpp src/IconProvider.h
    src/core/Types.cpp src/core/Types.h
    src/core/Store.cpp src/core/Store.h
    src/core/Backoff.h
    src/core/ApiClient.cpp src/core/ApiClient.h
    src/core/Transport.h
    src/core/RealtimeClient.cpp src/core/RealtimeClient.h
    src/core/Sync.cpp src/core/Sync.h
    src/core/Format.cpp src/core/Format.h
    src/core/MessageListModel.cpp src/core/MessageListModel.h
    src/core/ConversationListModel.cpp src/core/ConversationListModel.h
)
target_include_directories(luma_core PUBLIC src src/core src/app)
target_link_libraries(luma_core PUBLIC Qt6::Core Qt6::Gui Qt6::Quick Qt6::Network Qt6::WebSockets Qt6::Sql Qt6::Svg)

# ── App ────────────────────────────────────────────────────────────────────────
qt_add_executable(luma src/main.cpp src/app/AppController.cpp src/app/AppController.h)
set_target_properties(luma PROPERTIES OUTPUT_NAME luma)
target_link_libraries(luma PRIVATE luma_core Qt6::QuickControls2 Qt6Keychain::Qt6Keychain)
target_compile_definitions(luma PRIVATE PROJECT_VERSION_STRING="${PROJECT_VERSION}")

set_source_files_properties(${LUMA_DESIGN_DIR}/LumaTokens.qml PROPERTIES
    QT_QML_SINGLETON_TYPE TRUE
    QT_RESOURCE_ALIAS LumaTokens.qml)

file(GLOB LUMA_ICONS RELATIVE ${CMAKE_CURRENT_SOURCE_DIR} assets/icons/*.svg)
qt_add_qml_module(luma
    URI Luma
    VERSION 1.0
    QML_FILES
        ${LUMA_DESIGN_DIR}/LumaTokens.qml
        qml/Main.qml
        qml/SignIn.qml
        qml/ChatsPane.qml
        qml/ChatView.qml
        qml/Rail.qml
        qml/NewConversation.qml
        qml/components/Icon.qml
        qml/components/IconButton.qml
        qml/components/Avatar.qml
        qml/components/Badge.qml
        qml/components/LumaLine.qml
        qml/components/StatusNote.qml
        qml/components/LumaTextField.qml
        qml/components/LumaButton.qml
        qml/components/EmptyState.qml
        qml/components/LumaGlyph.qml
        qml/components/ConversationRow.qml
        qml/components/MessageDelegate.qml
        qml/components/TypingBubble.qml
        qml/components/Composer.qml
        qml/components/AccountButton.qml
    RESOURCES
        assets/fonts/InstrumentSans.ttf
        ${LUMA_ICONS}
)

# ── Tests ──────────────────────────────────────────────────────────────────────
enable_testing()
function(luma_test name)
    qt_add_executable(${name} tests/${name}.cpp)
    target_link_libraries(${name} PRIVATE luma_core Qt6::Test)
    target_compile_definitions(${name} PRIVATE
        LUMA_ICON_DIR="${CMAKE_CURRENT_SOURCE_DIR}/assets/icons"
        LUMA_VECTORS_DIR="${CMAKE_CURRENT_SOURCE_DIR}/../protocol/vectors")
    add_test(NAME ${name} COMMAND ${name})
    set_tests_properties(${name} PROPERTIES ENVIRONMENT "QT_QPA_PLATFORM=offscreen")
endfunction()
luma_test(tst_icons)
luma_test(tst_store)
luma_test(tst_backoff)
luma_test(tst_sync)
luma_test(tst_e2e)
luma_test(tst_models)
