# Add app-specific R8 rules here as the codebase grows.

# Hev JNI_OnLoad resolves this exact class name at runtime.
-keep class hev.htproxy.TProxyService { *; }
