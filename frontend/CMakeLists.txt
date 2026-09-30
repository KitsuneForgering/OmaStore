cmake_minimum_required(VERSION 3.21)
project(omastore-gui VERSION 0.1.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_AUTOMOC ON)
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

find_package(Qt6 6.5 REQUIRED COMPONENTS Core Gui Network Quick QuickControls2 Test)
qt_standard_project_setup(REQUIRES 6.5)
qt_policy(SET QTP0004 NEW)

# C++ logic without QML (IPC client, models, theme), used by the app and the tests.
add_library(omastore-core STATIC
    src/rpcclient.h src/rpcclient.cpp
    src/catalogmodel.h src/catalogmodel.cpp
    src/jobsmodel.h src/jobsmodel.cpp
    src/backend.h src/backend.cpp
    src/theme.h src/theme.cpp
    src/imageprovider.h src/imageprovider.cpp
    src/offlinenam.h src/offlinenam.cpp
    src/markdown.h src/markdown.cpp
)
target_include_directories(omastore-core PUBLIC src)
target_link_libraries(omastore-core PUBLIC Qt6::Core Qt6::Gui Qt6::Network Qt6::Quick)
target_compile_options(omastore-core PRIVATE -Wall -Wextra)

qt_add_executable(omastore-gui src/main.cpp)

qt_add_qml_module(omastore-gui
    URI OmaStore
    VERSION 1.0
    QML_FILES
        qml/Main.qml
        qml/CatalogPage.qml
        qml/DetailPage.qml
        qml/AppCard.qml
        qml/AppIcon.qml
        qml/JobsBar.qml
        qml/SideBar.qml
        qml/PrimaryButton.qml
)

target_link_libraries(omastore-gui PRIVATE omastore-core Qt6::QuickControls2)

include(GNUInstallDirs)
install(TARGETS omastore-gui RUNTIME DESTINATION ${CMAKE_INSTALL_BINDIR})

enable_testing()
foreach(t rpcclient models theme markdown ui)
    qt_add_executable(tst_${t} tests/tst_${t}.cpp tests/fakedaemon.h)
    target_link_libraries(tst_${t} PRIVATE omastore-core Qt6::Test)
    if(t STREQUAL "ui")
        target_compile_definitions(tst_ui PRIVATE OMASTORE_QML_DIR="${CMAKE_CURRENT_SOURCE_DIR}/qml")
    endif()
    add_test(NAME ${t} COMMAND tst_${t})
    set_tests_properties(${t} PROPERTIES ENVIRONMENT "QT_QPA_PLATFORM=offscreen")
endforeach()
