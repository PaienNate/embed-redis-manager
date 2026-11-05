git@github.com:qishibo/AnotherRedisDesktopManager.git
需求：fork AnotherRedisDesktopManager 并实现一个Go的依赖库，它满足如下功能：
1. 集成并修改AnotherRedisDesktopManager的代码，让他的后端在Go上。
2. 库将UI以及对应的接口注册到对应的HTTP MUX上。从而支持在任意Go项目中初始化Client。
3. 库内提供注入Redis Client的能力，页面仍提供Redis Client增加的对应页面，可以通过库的功能开启或者关闭它。
4. 最终结果是，这个依赖库可以引用到其他的go程序里，并通过配置注册（比如注册对方go项目已经初始化好的client），
进而提供内嵌的Client页面，实现在特殊环境（如redis难以调试、不提供外网地址、内网账号权限不统一）下仍然可以进行数据库调整的目的。