"""commands 包 — 保持 werewolf_game.py 的导入兼容。"""

from .base import process_night_death, GAME_FILE, CONFIG_FILE
from .init import cmd_init, cmd_reset
from .night import cmd_night, cmd_night_auto
from .day import cmd_day, cmd_day_auto, cmd_vote
from .special import cmd_hunter_shot, cmd_explode
from .sheriff import cmd_sheriff, cmd_sheriff_direction
from .info import cmd_status, cmd_status_pretty, cmd_summary, cmd_stats, cmd_hint, cmd_journal
from .config import cmd_config
from .prompts import cmd_make_prompts
from .replay import cmd_replay
from .rawlog import cmd_log_raw
