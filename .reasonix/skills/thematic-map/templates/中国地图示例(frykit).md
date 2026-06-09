# 中国地图示例(frykit)

这是frykit库官方提供的示例代码，其中使用了很多的frykit内置数据。

```python
"""
中国绘制等值线填色图，添加南海小地图，并用国界裁剪填色图
"""

from typing import cast

import matplotlib.pyplot as plt
import numpy as np
from cartopy.feature import LAND
from cartopy.mpl.geoaxes import GeoAxes
from scipy.ndimage import gaussian_filter
from cartopy import crs as ccrs
import frykit.plot as fplt

# 读取数据
data = fplt.load_test_data()
X, Y = np.meshgrid(data.lon, data.lat)
Z = gaussian_filter(data.t2m - 273.15, sigma=1)

# 设置投影
map_crs = ccrs.AlbersEqualArea(
    central_longitude=110,
    standard_parallels=(25, 47)
)
data_crs = fplt.PLATE_CARREE

# 设置刻度
xticks = np.arange(-180, 181, 10)
yticks = np.arange(-90, 91, 10)

# 准备大地图
fig = plt.figure(figsize=(10, 6))
main_ax = fig.add_subplot(projection=map_crs)
main_ax = cast(GeoAxes, main_ax)
fplt.set_map_ticks(main_ax, (80, 136, 13, 54), xticks, yticks)
main_ax.gridlines(xlocs=xticks, ylocs=yticks, lw=0.5, ls="--", color="gray")

# 类似 NCL 的刻度风格
main_ax.tick_params(
    length=8,
    width=0.9,
    labelsize=8,
    top=True,
    right=True,
    labeltop=True,
    labelright=True,
)

# 准备小地图
mini_ax = fplt.add_mini_axes(main_ax)
mini_ax = cast(GeoAxes, mini_ax)
mini_ax.set_extent((107, 122, 2, 25), data_crs)
mini_ax.gridlines(xlocs=xticks, ylocs=yticks, lw=0.5, ls="--", color="gray")

# 添加要素
land = LAND.with_scale("50m")
for ax in [main_ax, mini_ax]:
    ax.set_facecolor("skyblue")
    ax.add_feature(LAND, fc="floralwhite", ec="k", lw=0.5)
    fplt.add_cn_province(ax, lw=0.3)
    fplt.add_cn_line(ax, lw=0.5)

# 绘制填色图
for ax in [main_ax, mini_ax]:
    cf = ax.contourf(
        X,
        Y,
        Z,
        levels=np.linspace(0, 32, 50),
        cmap="turbo",
        extend="both",
        transform=data_crs,
        transform_first=True,
    )
    fplt.clip_by_cn_border(cf)

# 绘制 colorbar
cbar = plt.colorbar(
    cf,  # pyright: ignore[reportPossiblyUnboundVariable]
    ax=main_ax,
    ticks=np.linspace(0, 32, 9),
    shrink=0.6,
    pad=0.1,
    aspect=30,
    extendfrac=0,
    orientation="horizontal",
)
cbar.ax.tick_params(length=4, labelsize=8)

# 大地图添加指北针和比例尺
fplt.add_compass(main_ax, 0.92, 0.85, size=15, style="star")
scale_bar = fplt.add_scale_bar(main_ax, 0.05, 0.1, length=1000)
scale_bar.set_xticks([0, 500, 1000])
scale_bar.xaxis.get_label().set_fontsize("small")

# 小地图添加比例尺
scale_bar = fplt.add_scale_bar(mini_ax, 0.4, 0.15, length=500)
scale_bar.set_xticks([0, 500])
scale_bar.xaxis.get_label().set_fontsize("small")

# 设置标题
main_ax.set_title(
    "2m Temperature (\N{DEGREE CELSIUS})",
    y=1.1,
    fontsize="large",
    weight="bold",
)

# 保存图片
fplt.savefig("contourf.png")
plt.close(fig)


```
