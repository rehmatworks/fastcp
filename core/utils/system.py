from core.utils import filesystem



def setup_website(website: object):
    """Setup website.
    
    This function is responsible to setup the website when it's created and it
    restarts the services. Ideally, this function should be called soon after
    the website model is created.
    """
    
    # Create initial directories
    filesystem.create_website_dirs(website)
    
    # Create FPM pool conf
    filesystem.generate_fpm_conf(website)


def delete_website(website: object):
    """Delete website.
    
    This function cleans the website data and it should be called right before
    the website model is about to be deleted.
    """
    
    # Delete website directories
    filesystem.delete_website_dirs(website)
    
    # Delete PHP FPM pool conf
    filesystem.delete_fpm_conf(website)
    
    # Delete NGINX vhost files
    filesystem.delete_nginx_vhost(website)