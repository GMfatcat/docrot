cmake_minimum_required(VERSION 3.20)
project(fixture_native VERSION 0.1 LANGUAGES C CXX)

add_library(fixnative native/src/fix.c)
add_executable(fixcli native/src/main.cpp)
add_custom_target(docs COMMAND echo docs)
