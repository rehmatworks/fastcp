import psutil
from core.models import Website, Database
from datetime import datetime


MEMORY = psutil.virtual_memory()
DISK = psutil.disk_usage('/')

def system_stats():
    """Returns system stats.
    
    This function attempts to determine the system resources like disk usage, RAM usage,
    etc. and returns the stats as a dict.
    
    Returns:
        dict: A dictionary that contains the info on system resources.
    """
    return {
        'ram': {
            'memory': {
                'total': MEMORY.total,
                'percent': MEMORY.percent,
            }
        },
        'disk': {
            'total': DISK.total,
            'percent': DISK.percent
        },
        'stats': {
            'websites': Website.objects.count(),
            'databases': Database.objects.count()
        }
    }

def hardware_info():
    """Returns hardware information."""
    swap = psutil.swap_memory()
    uptime = datetime.now() - datetime.fromtimestamp(psutil.boot_time())
    return {
        'uptime': str(uptime).split('.')[0],
        'ram': {
            'memory': {
                'total': MEMORY.total,
                'percent': MEMORY.percent,
            },
            'swap': {
                'total': swap.total,
                'percent': swap.percent
            }
        },
        'cpu': {
            'logical': psutil.cpu_count(),
            'physical': psutil.cpu_count(logical=False),
            'load': psutil.getloadavg()
        },
        'disk': {
            'total': DISK.total,
            'percent': DISK.percent
        },
    }
    