#include "stdbool.h"

extern void systray_ready();
extern void systray_on_exit();
extern void systray_left_click();
extern void systray_right_click();
extern void systray_menu_item_selected(int menu_id);
extern void systray_menu_will_open();
void registerSystray(void);
void nativeEnd(void);
int nativeLoop(void);
void nativeStart(void);

void setIcon(const char* iconBytes, int length, bool template);
void setMenuItemIcon(const char* iconBytes, int length, int menuId, bool template);
void setTitle(char* title);
void setTooltip(char* tooltip);
void setRemovalAllowed(bool allowed);
void add_or_update_menu_item(int menuId, int parentMenuId, char* title, char* tooltip, short disabled, short checked, short isCheckable);
void add_separator(int menuId, int parentId);
void hide_menu_item(int menuId);
void remove_menu_item(int menuId);
void show_menu_item(int menuId);
void reset_menu();
void show_menu();
void quit();
