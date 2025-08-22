from django.conf import settings


def general_settings(request):
    """General settings.
    
    This function is used as a context processor and it feeds the Django templates
    with some common settings.
    
    Args:
        request: Django HTTP request object.
    
    Returns:
        dict: A dictionary of general settings.
    """
    return {
        'FASTCP_SITE_NAME': settings.FASTCP_SITE_NAME,
        'FASTCP_SITE_URL': settings.FASTCP_SITE_URL,
        'FASTCP_FM_ROOT': settings.FILE_MANAGER_ROOT,
        'FASTCP_VERSION': settings.FASTCP_VERSION,
    # Prefer explicit FASTCP_PHPMYADMIN_URL if set, otherwise build from server IP
    'PMA_URL': getattr(settings, 'FASTCP_PHPMYADMIN_URL', f'https://{settings.SERVER_IP_ADDR}/phpmyadmin')
    }