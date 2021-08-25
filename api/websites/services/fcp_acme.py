from cryptography.hazmat.backends import default_backend
from cryptography.hazmat.primitives.asymmetric import rsa
import josepy as jose
import OpenSSL
import os
from acme import challenges
from acme import client
from acme import crypto_util
from acme import messages

# Directory URLs
DIRECTORY_URL = 'https://acme-staging-v02.api.letsencrypt.org/directory'
STAGING_DIRECTORY_URL = 'https://acme-v02.api.letsencrypt.org/directory'

# Our user agent
USER_AGENT = 'FastCP (+https://fastcp.org)'

# Account key size
ACC_KEY_BITS = 2048

# Certificate private key size
CERT_PKEY_BITS = 2048


class FastcpAcme(object):
    """FastCP ACME class

    This class is responsible to generate SSL certificates using Let's Encrypt ACME API v2.0.
    """

    def __init__(self, acc_key: str = None, regr: str = None, staging: bool = False):
        """Create ACME client.

        This method sets the ACME client and prepares the initial configuration.

        Args:
            acc_key (str): Account key as JSON string.
            regr (str): Existing account as a JSON string if already created.
            staging (bool): Specifies either the staging directory URL should be used the production URL.
        """
        
        # Generate account key if not provided, otherwise load it from the
        # provided string
        if not acc_key:
            acc_key = self._generate_acc_key()
        else:
            acc_key = jose.JWK.json_loads(acc_key)

        self.acc_key = acc_key
        
        # Load account if provided
        if regr:
            regr = messages.RegistrationResource.json_loads(regr)
            
        self.regr = regr        

        if staging:
            dir_url = STAGING_DIRECTORY_URL
        else:
            dir_url = DIRECTORY_URL

        net = client.ClientNetwork(self.acc_key, account=self.regr, user_agent=USER_AGENT)
        directory = messages.Directory.from_json(net.get(dir_url).json())
        self.client = client.ClientV2(directory, net=net)
        
        # Register account if not exists
        if not self.regr:
            regr = self._register_account()

        self.regr = regr

    def _generate_acc_key(self) -> object:
        """Generate account key.

        Generates an account key.

        Returns:
            object: Returns JWKRSA object.
        """
        return jose.JWKRSA(key=rsa.generate_private_key(
            public_exponent=65537, key_size=ACC_KEY_BITS, backend=default_backend()))

    def _register_account(self, email: str = None):
        """Registers account.

        Registers an ACME account for the provided email address.

        Args:
            email (str): The email address to register an account for.
        """
        if email:
            email = (email)
              
        return self.client.new_account(
            messages.NewRegistration.from_data(email=None, terms_of_service_agreed=True))

    def _generate_csr(self, domains: list, priv_key: str = None) -> tuple:
        """Create certificate signing request.

        Generates a certificate signing request. If private key is not provided, it will be created too.

        Params:
            domains (list): The domain names to generate the CSR (and private key) for.
            priv_key (str): The private key. If None, a private key will be generated as well.

        Returns:
            tuple: Returns a tuple with private key at index 0 and with CSR at index 1
        """

        if priv_key is None:
            # Create private key if not provided
            pkey = OpenSSL.crypto.PKey()
            pkey.generate_key(OpenSSL.crypto.TYPE_RSA, CERT_PKEY_BITS)
            priv_key = OpenSSL.crypto.dump_privatekey(OpenSSL.crypto.FILETYPE_PEM,
                                                      pkey)
        csr_pem = crypto_util.make_csr(priv_key, domains)
        return priv_key, csr_pem

    def _select_chall(self, orderr: object, chall_type='http') -> object:
        """Extract authorization resource from within order resource.

        Args:
            orderr (object): ACME new order object (for new certs or for a renewal).
            chall_type (str): The challenge type to choose from the CA server's supported challenge types.

        Returns:
            object: Returns the challange object.
        """

        authz_list = orderr.authorizations

        chall_list = []
        
        for authz in authz_list:
            # Choosing challenge.
            # authz.body.challenges is a set of ChallengeBody objects.
            for i in authz.body.challenges:
                # Find the supported challenge.
                if chall_type == 'http':
                    if isinstance(i.chall, challenges.HTTP01):
                        chall_list.append(i)
        
        return chall_list

    def request_ssl(self, domains: list, email: str = None, priv_key: str = None, chall_type='http') -> dict:
        """Create a new SSL certificate request.

        This creates a new order for an SSL certificate for the provided domains. This method doesn't
        obtain the final SSL. It just places a request order and it returns the tokens to verify the
        ownership of the domain. Final SSL certificate files can be obtained using get_ssl() method.
        
        To renew an existing certificate, the private key associated to that certificate should be provided.

        Args:
            domains (list): List of domain names to get SSL cert for.
            priv_key (str): Private key as a string. If not provided, it will be generated
                            along the CSR.
            chall_type (str): Challenge type. It defaults to HTTP challenge.

        Returns:
            dict: Containing the challenge HTTP path and the auth token.
        """
        
        # Create a CSR and priv key
        priv_key, csr = self._generate_csr(domains, priv_key=priv_key)
        self.priv_key = priv_key

        # Place an ACME order
        self.acme_order = self.client.new_order(csr)

        # Get challenge path & token
        self.challs_list = []
        token_paths = []
        for chall in self._select_chall(self.acme_order, chall_type=chall_type):
            self.challs_list.append(chall)
            response, validation = chall.chall.response_and_validation(
                account_key=self.acc_key)
            self.response = response

            # Get challenge path
            challange_path = os.path.join(
                challenges.HTTP01.URI_ROOT_PATH, chall.chall.encode('token'))
            
            # Challenge token
            challenge_token = validation.encode()
            
            token_paths.append({
                'path': challange_path,
                'token': challenge_token
            })
        
        return token_paths
        
    def get_ssl(self):
        """Get SSL certificate files.
        
        This method gets the final SSL certificate files and it should be called only after
        completing the validation steps, i.e. placing the auth tokens in webroot.
        
        Returns:
            dict: Dict containing SSL certificates and the private key.
        """
        try:
            for chall in self.challs_list:
                self.client.answer_challenge(chall, self.response)
            order_result = self.client.poll_and_finalize(self.acme_order)
            return {
                'full_chain': order_result.fullchain_pem,
                'priv_key': self.priv_key
            }
        except:
            return False