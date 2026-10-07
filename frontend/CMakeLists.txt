cmake_minimum_required(VERSION 3.21)
project(omastore-gui LANGUAGES CXX)

# The version the interface reports (in the bug report it builds). The Makefile
# passes the release tag; a build configured by hand takes the last tag of the
# checkout. Never hardcoded here: a forgotten bump is a release that lies about
# its own version.
if(NOT OMASTORE_VERSION)
  set(described 1)
  find_package(Git QUIET)
  if(Git_FOUND)
    execute_process(COMMAND git describe --tags --abbrev=0
      WORKING_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}"
      RESULT_VARIABLE described OUTPUT_VARIABLE described_tag
      OUTPUT_STRIP_TRAILING_WHITESPACE ERROR_QUIET)
  endif()
  if(described EQUAL 0)
    set(OMASTORE_VERSION "${described_tag}")
  endif()
  string(REGEX REPLACE "^v" "" OMASTORE_VERSION "${OMASTORE_VERSION}")
endif()
if(NOT OMASTORE_VERSION MATCHES "^[0-9]")
  set(OMASTORE_VERSION "dev")
endif()
message(STATUS "OmaStore version: ${OMASTORE_VERSION}")

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
    src/singleinstance.h src/singleinstance.cpp
)
target_include_directories(omastore-core PUBLIC src)
target_link_libraries(omastore-core PUBLIC Qt6::Core Qt6::Gui Qt6::Network Qt6::Quick)
target_compile_options(omastore-core PRIVATE -Wall -Wextra)
target_compile_definitions(omastore-core PUBLIC OMASTORE_VERSION="${OMASTORE_VERSION}")

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
        qml/ActionButton.qml
        qml/Badge.qml
        qml/LinkText.qml
        qml/PublishPage.qml
        qml/categories.js
)

target_link_libraries(omastore-gui PRIVATE omastore-core Qt6::QuickControls2)

# Translations (i18n/*.ts, updated with `make translations`) are compiled into
# the executable under :/i18n. Without Qt Linguist the interface is English only.
find_package(Qt6 QUIET COMPONENTS LinguistTools)
if(Qt6LinguistTools_FOUND)
    file(GLOB OMASTORE_TS_FILES CONFIGURE_DEPENDS ${CMAKE_CURRENT_SOURCE_DIR}/i18n/*.ts)
    qt_add_lrelease(omastore-gui TS_FILES ${OMASTORE_TS_FILES} QM_FILES_OUTPUT_VARIABLE OMASTORE_QM_FILES)
    qt_add_resources(omastore-gui "translations" PREFIX "/i18n" BASE ${CMAKE_CURRENT_BINARY_DIR}
                     FILES ${OMASTORE_QM_FILES})
else()
    message(WARNING "Qt Linguist tools not found (qt6-tools): the interface will be in English only")
endif()

include(GNUInstallDirs)
install(TARGETS omastore-gui RUNTIME DESTINATION ${CMAKE_INSTALL_BINDIR})

enable_testing()
foreach(t rpcclient models theme markdown singleinstance ui)
    qt_add_executable(tst_${t} tests/tst_${t}.cpp tests/fakedaemon.h)
    target_link_libraries(tst_${t} PRIVATE omastore-core Qt6::Test)
    if(t STREQUAL "ui")
        target_compile_definitions(tst_ui PRIVATE OMASTORE_QML_DIR="${CMAKE_CURRENT_SOURCE_DIR}/qml")
    endif()
    add_test(NAME ${t} COMMAND tst_${t})
    set_tests_properties(${t} PROPERTIES ENVIRONMENT "QT_QPA_PLATFORM=offscreen")
endforeach()
