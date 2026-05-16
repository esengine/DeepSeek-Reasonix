use ratatui::style::Color;

pub const BG: Color = Color::Rgb(0x0f, 0x10, 0x18);
pub const FG: Color = Color::Rgb(0xe8, 0xe9, 0xf3);
pub const FG1: Color = Color::Rgb(0xa8, 0xaa, 0xbd);
pub const FG2: Color = Color::Rgb(0x6b, 0x6e, 0x85);
pub const FG3: Color = Color::Rgb(0x3d, 0x40, 0x55);
pub const DS: Color = Color::Rgb(0x6b, 0x85, 0xff);
pub const DS_BRIGHT: Color = Color::Rgb(0x8b, 0x9f, 0xff);
pub const DS_PURPLE: Color = Color::Rgb(0xa7, 0x8b, 0xfa);
pub const OK: Color = Color::Rgb(0x5e, 0xea, 0xd4);
pub const WARN: Color = Color::Rgb(0xfb, 0xbf, 0x24);
pub const ERR: Color = Color::Rgb(0xfb, 0x71, 0x85);
pub const INFO: Color = Color::Rgb(0x60, 0xa5, 0xfa);

pub const LOGO: [&str; 6] = [
    "██████╗ ███████╗ █████╗ ███████╗ ██████╗ ███╗   ██╗██╗██╗  ██╗",
    "██╔══██╗██╔════╝██╔══██╗██╔════╝██╔═══██╗████╗  ██║██║╚██╗██╔╝",
    "██████╔╝█████╗  ███████║███████╗██║   ██║██╔██╗ ██║██║ ╚███╔╝ ",
    "██╔══██╗██╔══╝  ██╔══██║╚════██║██║   ██║██║╚██╗██║██║ ██╔██╗ ",
    "██║  ██║███████╗██║  ██║███████║╚██████╔╝██║ ╚████║██║██╔╝ ██╗",
    "╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝╚══════╝ ╚═════╝ ╚═╝  ╚═══╝╚═╝╚═╝  ╚═╝",
];

pub const SIDEBAR_WIDTH: u16 = 34;
pub const DOCK_HEIGHT: u16 = 5;
pub const COMPOSER_PLACEHOLDER: &str = "type to chat   / for commands   @ for files";
