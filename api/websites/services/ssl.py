from .fcp_acme import FastcpAcme
import requests
import os
from core.utils.filesystem import get_website_paths
from core.signals import restart_services
from core.models import Domain


# Verify path
FCP_VERIFY_PATH = '/.well-known/fastcp-verify.txt'
FCP_VERIFY_STR = 'fastcp' # This should be written to the verify file above by the installer
ACME_VERIFY_BASE_DIR = '/var/fastcp/well-known'

class FastcpSsl(object):
    """FastCP SSL.
    
    This class deals with Let's Encrypt SSL certificates for domains.
    """
    
    def is_resolving(self, domain: str) -> bool:
        """Check resolving or not.
        
        This method checks and verifies either the provided domain is resolving to the
        server IP or not. If it does, the verify path above should return a 200 status
        code with fastcp as text in response.
        
        Args:
            domain (str): The domain name.
        
        Returns:
            bool: True on success Falase otherwise.
        """
        
        try:
            res = requests.get(f'http://{domain}{FCP_VERIFY_PATH}', timeout=5)
            if res.status_code == 200 and res.text.strip() == FCP_VERIFY_STR:
                return True
        except Exception:
            # We aren't interested in the reason of failure
            pass
        
        return False

    
    def get_ssl(self, website, renew: bool = False) -> bool:
        """Get SSL.
        
        This method attempts to get SSL certificates for the provided domain names. An SSL
        is requested only if the domain is found to be resolving to the server IP, otherwise
        it is excluded from the list.
        
        First of all, SSL certs are requested from Let's Encrypt. If succeeded, SSL cert files
        are generated and SSL vhost file is created.
        
        Args:
            website (object): The website model object.
            renew (bool): Is this a renew reuest or not. If it's a renewal request, we will use
                            the existing private key.
        
        Returns:
            bool: True on success False otherwise.
        """
        try:
            verified_domains = []
            token_path = None
            
            for dom in website.domains.all():
                if self.is_resolving(dom.domain):
                    verified_domains.append(dom.domain)
                    
            
            # Get website paths
            paths = get_website_paths(website)
            if not os.path.exists(paths.get('ssl_base')):
                os.makedirs(paths.get('ssl_base'))
            
            if renew:
                with open(paths.get('priv_key_path')) as f:
                    priv_key = f.read()
            else:
                priv_key = None
            
            if len(verified_domains):
                acme = FastcpAcme(staging=True)
                
                # Initiate an order
                result = acme.request_ssl(domains=verified_domains, priv_key=priv_key)
                
                # Write the challenge token to path
                if result:
                    base_dir = os.path.join(ACME_VERIFY_BASE_DIR, 'acme-challenge')
                    if not os.path.exists(base_dir):
                        os.makedirs(base_dir)
                    
                    token_path = os.path.join(base_dir, os.path.basename(result.get('path')))
                    with open(token_path, 'wb') as f:
                        f.write(result.get('token'))
                
                # After the challange token is written, request SSL cert
                result = acme.get_ssl()
                
                if result:
                    # Write private key
                    with open(paths.get('priv_key_path'), 'wb') as f:
                        f.write(result.get('priv_key'))
                    
                    # Write cert chain
                    with open(paths.get('cert_chain_path'), 'w') as f:
                        f.write(str(result.get('full_chain')))
                        
                    # Restart NGINX
                    restart_services.send('nginx')
                    
                    # Update domains
                    for dom in verified_domains:
                        Domain.objects.filter(domain=dom).update(ssl=True)
                
                # Remove verification file
                if token_path and os.path.exists(token_path):
                    os.remove(token_path)
        except Exception as e:
            raise e
            # Not interested in the reason
            pass
                
        return False
                
